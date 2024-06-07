{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: syslog
      protocol.{{ item.protocol }}:
        host: "{{ item.host }}"

      {% if item.syslog_filters is defined and item.syslog_filters %}syslog_filters:{% for filter in item.syslog_filters %}
       - conditions:{% for condition in filter.conditions %}
         - key: {{ condition.key }}
           op: '{{ condition.op }}'
           value: '{{ condition.value }}'{% endfor %}
      {% endfor %}{% endif %}

      {% if item.output is defined and item.output %}{{ item.output.type }}: {{ item.output.params }}{% endif %}

{% endfor %}
{% endif %}