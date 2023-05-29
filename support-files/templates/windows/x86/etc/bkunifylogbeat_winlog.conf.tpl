{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: winlog
      delimiter: '{{ item.get('delimiter', '') }}'
      {% if item.filters is defined and item.filters %}filters:{% for filter in item.filters %}
       - conditions:{% for condition in filter.conditions %}
         - index: {{ condition.index | int }}
           key: '{{ condition.key }}'
           op: '{{ condition.op }}'{% endfor %}
      {% endfor %}{% endif %}
      {% if item.event_logs is defined and item.event_logs %}
      event_logs:{% for event_log in item.event_logs %}
        - name: {{ event_log.get("name", "") }}
          ignore_older: '{{ event_log.get("ignore_older", "72h") }}'
          level: '{{ event_log.get("level", "") }}'
          event_id: '{{ event_log.get("event_id", "") }}'
          {% if item.provider_name is defined and item.provider_name %}provider:{% for provider in item.provider_name %}
           - {{ provider }}{% endfor %}
          {% endif %}
      {% endfor %}
      {% endif %}
{% endfor %}{% endif %}
