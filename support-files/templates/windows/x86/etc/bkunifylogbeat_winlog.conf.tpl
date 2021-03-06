{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: winlog
      {% if item.event_logs is defined and item.event_logs %}
      event_logs:{% for event_log in item.event_logs %}
        - name: {{ event_log.get("name", "") }}
          ignore_older: '{{ event_log.get("ignore_older", "72h") }}'
          level: '{{ event_log.get("level", "") }}'
          event_id: '{{ event_log.get("event_id", "") }}'
      {% endfor %}
      {% endif %}
{% endfor %}{% endif %}
