package beater

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elastic/beats/filebeat/input/file"
	"github.com/stretchr/testify/require"

	cfg "github.com/TencentBlueKing/bkunifylogbeat/config"
)

func TestManagerStartRejectsTaskParseErrorBeforeStartingAnyTask(t *testing.T) {
	preparePreviewRegistrar(t)
	config := newPreviewTaskConfig()
	config.Tasks = append(config.Tasks, map[string]interface{}{
		"type": "kafka", "dataid": 456,
		"field_extraction": map[string]interface{}{"pattern": "["},
		"direct_read":      map[string]interface{}{"enabled": true},
	})
	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	calls := 0
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error { calls++; return nil }
	t.Cleanup(func() { _ = manager.Stop() })
	require.ErrorContains(t, manager.Start(), "field_extraction")
	require.Zero(t, calls)

	manager.isTestMode = func() bool { return false }
	require.NoError(t, manager.Start())
	require.Equal(t, 1, calls)
}

func TestDisabledDirectReadValidation(t *testing.T) {
	disabled := false
	task := newTaskConfigForPreview(t, "kafka", &disabled)
	preview := newPreviewManager(true, nil)
	require.NoError(t, preview.validateDirectTask(task))
	formal := newPreviewManager(false, nil)
	require.ErrorContains(t, formal.validateDirectTask(task), "only allowed with -T")
	preview.preview = true
	preview.previewTaskID = "other"
	require.Error(t, preview.validateDirectTask(task))
}

func TestDisabledDirectReadManagerStart(t *testing.T) {
	preparePreviewRegistrar(t)
	config := cfg.Config{Tasks: []interface{}{map[string]interface{}{
		"type": "kafka", "dataid": 789, "hosts": []string{"127.0.0.1:1"},
		"topics": []string{"preview"}, "group_id": "preview-test",
		"direct_read": map[string]interface{}{"enabled": false},
	}}}
	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	t.Cleanup(func() { _ = manager.Stop() })
	require.NoError(t, manager.Start())
	require.Len(t, manager.tasks, 1)
	require.Nil(t, manager.PreviewDone())
}

func TestManagedPreviewIgnoresReload(t *testing.T) {
	original := cfg.Config{MaxCpuLimit: 11}
	manager := &Manager{preview: true, config: original}
	manager.Reload(cfg.Config{MaxCpuLimit: 99})
	require.Equal(t, 11, manager.config.MaxCpuLimit)

	bt := &LogBeat{manager: manager, config: original}
	bt.Reload(nil)
	require.Equal(t, 11, bt.config.MaxCpuLimit)
}

func TestManagerStartToleratesNonPreviewParseError(t *testing.T) {
	preparePreviewRegistrar(t)
	config := newPreviewTaskConfig()
	config.Tasks = append(config.Tasks, map[string]interface{}{
		"type": "log", "dataid": 456,
		"field_extraction": map[string]interface{}{"pattern": "["},
	})
	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	calls := 0
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error { calls++; return nil }
	t.Cleanup(func() { _ = manager.Stop() })

	require.NoError(t, manager.Start())
	require.Equal(t, 1, calls)
}

func TestManagerStartStrictForPreviewPathWithUnrelatedError(t *testing.T) {
	preparePreviewRegistrar(t)
	config := newPreviewTaskConfig()
	config.Tasks = append(config.Tasks,
		map[string]interface{}{
			"type": "kafka", "dataid": 456,
			"hosts": []string{"127.0.0.1:9092"}, "topics": []string{"preview"},
			"direct_read": map[string]interface{}{"enabled": true, "max_events": 5, "timeout": "10s"},
		},
		map[string]interface{}{
			"type": "log", "dataid": 789,
			"field_extraction": map[string]interface{}{"pattern": "["},
		},
	)
	manager, err := NewManager(config, make(chan struct{}))
	require.NoError(t, err)
	manager.isTestMode = func() bool { return true }
	calls := 0
	manager.startTaskFunc = func(*cfg.TaskConfig, []file.State) error { calls++; return nil }
	t.Cleanup(func() { _ = manager.Stop() })

	require.ErrorContains(t, manager.Start(), "field_extraction")
	require.Zero(t, calls)
}

func TestHasEnabledDirectReadIntent(t *testing.T) {
	config := cfg.Config{Tasks: []interface{}{map[string]interface{}{
		"type": "log", "dataid": 1, "paths": []string{"/tmp/test.log"},
	}}}
	require.False(t, hasEnabledDirectReadIntent(config))

	config.Tasks = append(config.Tasks, map[string]interface{}{
		"type": "kafka", "dataid": 2,
		"hosts": []string{"127.0.0.1:9092"}, "topics": []string{"preview"},
		"direct_read": map[string]interface{}{"enabled": true},
	})
	require.True(t, hasEnabledDirectReadIntent(config))

	disabled := cfg.Config{Tasks: []interface{}{map[string]interface{}{
		"type": "kafka", "dataid": 3,
		"hosts": []string{"127.0.0.1:9092"}, "topics": []string{"preview"},
		"direct_read": map[string]interface{}{"enabled": false},
	}}}
	require.False(t, hasEnabledDirectReadIntent(disabled))

	dir := t.TempDir()
	path := filepath.Join(dir, "task.yml")
	require.NoError(t, os.WriteFile(path, []byte("local:\n  - type: kafka\n    dataid: 4\n    direct_read:\n      enabled: true\n"), 0600))
	secondary := cfg.Config{SecConfigs: []cfg.SecConfigItem{{Path: dir, Pattern: "*.yml"}}}
	require.True(t, hasEnabledDirectReadIntent(secondary))
}

func TestDisabledDirectReadRequiresKafka(t *testing.T) {
	disabled := false
	task := newTaskConfigForPreview(t, "log", &disabled)
	manager := newPreviewManager(true, nil)

	err := manager.validateDirectTask(task)
	require.ErrorContains(t, err, "requires a kafka task")

	tasks := map[string]*cfg.TaskConfig{task.ID: task}
	err = manager.preparePreview(tasks)
	require.ErrorContains(t, err, "requires a kafka task")
}
