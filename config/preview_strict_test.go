package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTasksStrictAndTolerantLoading(t *testing.T) {
	valid := map[string]interface{}{"type": "log", "dataid": 123, "paths": []string{"/tmp/test.log"}}
	invalid := map[string]interface{}{"type": "kafka", "dataid": 456, "field_extraction": map[string]interface{}{"pattern": "["}, "direct_read": map[string]interface{}{"enabled": true}}
	config := Config{Tasks: []interface{}{valid, invalid}}
	_, err := GetTasksStrict(config)
	require.ErrorContains(t, err, "task 1")
	require.ErrorContains(t, err, "field_extraction")
	require.Len(t, GetTasks(config), 1)

	tasks, err := GetTasksStrict(Config{})
	require.NoError(t, err)
	require.Empty(t, tasks)
}

func TestGetTasksStrictSecondaryErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	require.NoError(t, os.WriteFile(path, []byte("local:\n  - type: kafka\n    dataid: 123\n    field_extraction:\n      pattern: '['\n"), 0600))
	config := Config{SecConfigs: []SecConfigItem{{Path: dir, Pattern: "*.yml"}}}
	_, err := GetTasksStrict(config)
	require.ErrorContains(t, err, "bad.yml")
	require.ErrorContains(t, err, "field_extraction")
	require.Empty(t, GetTasks(config))

	require.NoError(t, os.WriteFile(path, []byte("local: [broken"), 0600))
	_, err = GetTasksStrict(config)
	require.ErrorContains(t, err, "bad.yml")

	_, err = GetTasksStrict(Config{SecConfigs: []SecConfigItem{{Path: dir, Pattern: "["}}})
	require.ErrorContains(t, err, "pattern")
	tasks, err := GetTasksStrict(Config{SecConfigs: []SecConfigItem{{Path: dir, Pattern: "*.missing"}}})
	require.NoError(t, err)
	require.Empty(t, tasks)
}
