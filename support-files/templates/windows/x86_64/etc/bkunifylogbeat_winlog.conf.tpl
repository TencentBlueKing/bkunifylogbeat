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

      {% if item.ext_meta is defined or item.labels is defined %}ext_meta:
      {%- if item.ext_meta is defined %}
      {%- for key, value in item.ext_meta.items() %}
        {{ key }}: "{{ value }}"
      {%- endfor %}
      {%- endif %}
      {%- if item.labels is defined %}
      {%- for label in item.labels %}
      {%- for key, value in label.items() %}
        {{ key }}: "{{ value }}"
      {%- endfor %}
      {%- endfor %}
      {%- endif %}
      {%- endif %}

      {% if item.output is defined and item.output %}{{ item.output.type }}: {{ item.output.params }}{% endif %}

{% endfor %}{% endif %}
