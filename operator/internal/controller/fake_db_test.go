package controller

import (
	"context"

	"github.com/0lawale/configmirror-operator/internal/database"
)

// FakeDB satisfies the DBClient interface without touching real AWS/RDS.
// Used in all unit tests.
type FakeDB struct{}

func (f *FakeDB) UpsertMirroredConfigMap(_ context.Context, _ database.MirrorRecord) error {
	return nil
}

func (f *FakeDB) DeleteMirroredConfigMap(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (f *FakeDB) DeleteAllForMirror(_ context.Context, _, _ string) error {
	return nil
}
