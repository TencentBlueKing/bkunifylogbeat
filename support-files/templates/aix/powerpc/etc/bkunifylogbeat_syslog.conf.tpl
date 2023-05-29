{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: syslog
      protocol.{{ item.protocol }}:
        host: "{{ item.host }}"
{% endfor %}
{% endif %}