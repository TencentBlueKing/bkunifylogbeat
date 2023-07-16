{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: syslog
      protocol.{{ item.protocol }}:
        host: "{{ item.host }}"

      {% if item.output is defined and item.output %}
      {{ item.output.type }}: {{ item.output.params }}
      {% endif %}

{% endfor %}
{% endif %}