package db

import (
    "github.com/stretchr/testify/require"
    "testing"
)

func TestFileModel(t *testing.T) {
    assert := require.New(t)

    f := File{}

    assert.Nil(f.Load())
}
