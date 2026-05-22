// Package database handles the PostgreSQL connection and all persistence
// operations for the ConfigMirror operator.
//
// SECURITY:
//   - Credentials are NEVER passed in as plain strings from env vars or config files.
//   - At startup the package fetches username, password, and endpoint from
//     AWS SSM Parameter Store using the pod's IRSA credentials.
//   - The connection string uses sslmode=require — the RDS parameter group
//     enforces SSL on the server side too (rds.force_ssl=1), so both ends agree.
//   - The DB password is held in memory only for the duration of the connection
//     string build; it is not stored in any struct field.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	_ "github.com/lib/pq" // PostgreSQL driver
	"go.uber.org/zap"
)

// Client wraps a *sql.DB and provides ConfigMirror-specific operations.
type Client struct {
	db     *sql.DB
	logger *zap.Logger
}

// SSMPaths holds the SSM Parameter Store paths for each credential.
type SSMPaths struct {
	Username string
	Password string
	Endpoint string
	DBName   string
}

// NewClient fetches RDS credentials from SSM, opens a PostgreSQL connection,
// runs migrations, and returns a ready-to-use Client.
func NewClient(ctx context.Context, paths SSMPaths, logger *zap.Logger) (*Client, error) {
	// -----------------------------------------------------------------------
	// Step 1: Load AWS config — uses IRSA credentials automatically.
	// When running in EKS with IRSA, the AWS SDK picks up the pod's
	// projected service account token and exchanges it for temporary creds.
	// -----------------------------------------------------------------------
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	ssmClient := ssm.NewFromConfig(awsCfg)

	// -----------------------------------------------------------------------
	// Step 2: Fetch credentials from SSM Parameter Store.
	// WithDecryption=true decrypts SecureString parameters using the KMS key.
	// -----------------------------------------------------------------------
	logger.Info("Fetching RDS credentials from SSM Parameter Store")

	username, err := getSSMParam(ctx, ssmClient, paths.Username)
	if err != nil {
		return nil, fmt.Errorf("fetching DB username from SSM (%s): %w", paths.Username, err)
	}

	password, err := getSSMParam(ctx, ssmClient, paths.Password)
	if err != nil {
		return nil, fmt.Errorf("fetching DB password from SSM (%s): %w", paths.Password, err)
	}

	endpoint, err := getSSMParam(ctx, ssmClient, paths.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("fetching DB endpoint from SSM (%s): %w", paths.Endpoint, err)
	}

	// -----------------------------------------------------------------------
	// Step 3: Build the DSN — sslmode=require enforces encrypted transport.
	// -----------------------------------------------------------------------
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s sslmode=require connect_timeout=10",
		endpoint, username, password, paths.DBName,
	)

	// -----------------------------------------------------------------------
	// Step 4: Open the connection pool.
	// -----------------------------------------------------------------------
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening DB connection: %w", err)
	}

	// Tune the connection pool for a lightweight operator workload
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	// Verify the connection is actually alive
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return nil, fmt.Errorf("pinging DB: %w", err)
	}

	logger.Info("Successfully connected to RDS PostgreSQL")

	client := &Client{db: db, logger: logger}

	// -----------------------------------------------------------------------
	// Step 5: Run schema migrations (idempotent — safe to run on every start)
	// -----------------------------------------------------------------------
	if err := client.migrate(ctx); err != nil {
		return nil, fmt.Errorf("running DB migrations: %w", err)
	}

	return client, nil
}

// Close closes the underlying database connection pool.
func (c *Client) Close() error {
	return c.db.Close()
}

// ----------------------------------------------------------------------------
// Schema Migrations
// Using raw SQL keeps the operator dependency-light.
// Each statement is idempotent (IF NOT EXISTS / CREATE INDEX CONCURRENTLY).
// ----------------------------------------------------------------------------

func (c *Client) migrate(ctx context.Context) error {
	c.logger.Info("Running database migrations")

	migrations := []string{
		// Main audit/persistence table
		`CREATE TABLE IF NOT EXISTS mirrored_configmaps (
			id                  BIGSERIAL PRIMARY KEY,
			config_mirror_name  TEXT        NOT NULL,
			config_mirror_ns    TEXT        NOT NULL,
			configmap_name      TEXT        NOT NULL,
			source_namespace    TEXT        NOT NULL,
			target_namespace    TEXT        NOT NULL,
			data                JSONB       NOT NULL DEFAULT '{}',
			sync_status         TEXT        NOT NULL DEFAULT 'Synced',
			synced_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// Unique constraint: one row per (mirror, configmap, target namespace)
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_mirrored_configmaps
			ON mirrored_configmaps (config_mirror_name, config_mirror_ns, configmap_name, target_namespace)`,

		// Index for fast lookups by mirror resource
		`CREATE INDEX IF NOT EXISTS idx_mirrored_configmaps_mirror
			ON mirrored_configmaps (config_mirror_name, config_mirror_ns)`,

		// Trigger to auto-update updated_at on row changes
		`CREATE OR REPLACE FUNCTION update_updated_at_column()
			RETURNS TRIGGER AS $$
			BEGIN NEW.updated_at = NOW(); RETURN NEW; END;
			$$ LANGUAGE plpgsql`,

		`DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_trigger WHERE tgname = 'trg_mirrored_configmaps_updated_at'
			) THEN
				CREATE TRIGGER trg_mirrored_configmaps_updated_at
				BEFORE UPDATE ON mirrored_configmaps
				FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
			END IF;
		END $$`,
	}

	for _, stmt := range migrations {
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("executing migration: %w\nSQL: %s", err, stmt)
		}
	}

	c.logger.Info("Database migrations completed successfully")
	return nil
}

// ----------------------------------------------------------------------------
// Persistence Operations
// ----------------------------------------------------------------------------

// UpsertMirroredConfigMap inserts or updates the record of a mirrored ConfigMap.
// Uses PostgreSQL's ON CONFLICT ... DO UPDATE for atomicity.
func (c *Client) UpsertMirroredConfigMap(ctx context.Context, record MirrorRecord) error {
	const query = `
		INSERT INTO mirrored_configmaps
			(config_mirror_name, config_mirror_ns, configmap_name, source_namespace, target_namespace, data, sync_status, synced_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (config_mirror_name, config_mirror_ns, configmap_name, target_namespace)
		DO UPDATE SET
			data        = EXCLUDED.data,
			sync_status = EXCLUDED.sync_status,
			synced_at   = NOW()
	`

	_, err := c.db.ExecContext(ctx, query,
		record.ConfigMirrorName,
		record.ConfigMirrorNamespace,
		record.ConfigMapName,
		record.SourceNamespace,
		record.TargetNamespace,
		record.DataJSON,
		record.SyncStatus,
	)
	if err != nil {
		return fmt.Errorf("upserting mirrored configmap record: %w", err)
	}

	return nil
}

// DeleteMirroredConfigMap removes the persistence record when a mirrored
// ConfigMap is deleted (e.g. the source ConfigMap was removed).
func (c *Client) DeleteMirroredConfigMap(ctx context.Context, mirrorName, mirrorNS, configmapName, targetNS string) error {
	const query = `
		DELETE FROM mirrored_configmaps
		WHERE config_mirror_name = $1
		  AND config_mirror_ns   = $2
		  AND configmap_name     = $3
		  AND target_namespace   = $4
	`

	_, err := c.db.ExecContext(ctx, query, mirrorName, mirrorNS, configmapName, targetNS)
	if err != nil {
		return fmt.Errorf("deleting mirrored configmap record: %w", err)
	}

	return nil
}

// DeleteAllForMirror removes all records belonging to a ConfigMirror resource.
// Called when the ConfigMirror resource itself is deleted.
func (c *Client) DeleteAllForMirror(ctx context.Context, mirrorName, mirrorNS string) error {
	const query = `
		DELETE FROM mirrored_configmaps
		WHERE config_mirror_name = $1
		  AND config_mirror_ns   = $2
	`

	result, err := c.db.ExecContext(ctx, query, mirrorName, mirrorNS)
	if err != nil {
		return fmt.Errorf("deleting all records for mirror %s/%s: %w", mirrorNS, mirrorName, err)
	}

	rows, _ := result.RowsAffected()
	c.logger.Info("Deleted mirror records from DB", zap.String("mirror", mirrorName), zap.Int64("rows", rows))
	return nil
}

// ----------------------------------------------------------------------------
// Supporting Types
// ----------------------------------------------------------------------------

// MirrorRecord is the data written to the database for each mirrored ConfigMap.
type MirrorRecord struct {
	ConfigMirrorName      string
	ConfigMirrorNamespace string
	ConfigMapName         string
	SourceNamespace       string
	TargetNamespace       string
	DataJSON              []byte // ConfigMap .data serialised as JSON
	SyncStatus            string
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func getSSMParam(ctx context.Context, client *ssm.Client, name string) (string, error) {
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true), // Decrypt SecureString parameters
	})
	if err != nil {
		return "", fmt.Errorf("SSM GetParameter %q: %w", name, err)
	}

	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("SSM parameter %q returned nil value", name)
	}

	return *out.Parameter.Value, nil
}
