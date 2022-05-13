logging.level: error
max_procs: 1
output.bkpipe:
  endpoint: {{ plugin_path.endpoint }}
{%- if nodeman is defined %}
  hostip: {{ nodeman.host.inner_ip }}
{% else %}
  hostip: {{ cmdb_instance.host.bk_host_innerip }}
{%- endif %}
  cloudid: {{ cmdb_instance.host.bk_cloud_id[0].id if cmdb_instance.host.bk_cloud_id is iterable and cmdb_instance.host.bk_cloud_id is not string else cmdb_instance.host.bk_cloud_id }}

path.logs: {{ plugin_path.log_path }}
path.data: {{ plugin_path.data_path }}
path.pid: {{ plugin_path.pid_path }}

# Internal queue configuration for buffering events to be published.
queue:
  mem:
    events: 1024
    flush.min_events: 0
    flush.timeout: "1s"

# monitoring reporter.
xpack.monitoring.enabled: true
xpack.monitoring.bkpipe:
  bk_biz_id: {{ plugin_monitoring.get("bk_biz_id", 2) if plugin_monitoring else 2 | int }}
  dataid: 1100006
  task_dataid: 1100007
  period: "30s"

processors:
  - drop_event:
      when:
        not:
          has_fields: ["dataid"]

bkunifylogbeat.eventdataid: -1
bkunifylogbeat.registry.flush: "10s"
bkunifylogbeat.multi_config:
  - path: {{ plugin_path.subconfig_path }}
    file_pattern: "*.conf"
  - path: {{ plugin_path.subconfig_path }}\bcs
    file_pattern: "*.conf"
