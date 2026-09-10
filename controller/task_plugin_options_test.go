package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
)

func TestSortTaskPluginBindOptionsOrdersByPriorityThenKey(t *testing.T) {
	metas := []jsplugin.Meta{
		{Key: "beta", SortPriority: 0},
		{Key: "alpha", SortPriority: 10},
		{Key: "gamma", SortPriority: 10},
		{Key: "delta", SortPriority: -1},
	}

	sortTaskPluginBindOptions(metas)

	assert.Equal(t, []string{"alpha", "gamma", "beta", "delta"}, []string{
		metas[0].Key, metas[1].Key, metas[2].Key, metas[3].Key,
	})
}
