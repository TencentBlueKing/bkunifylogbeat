{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: redis
      hosts: {%for host in item.hosts %}
        - '{{ host }}'{% endfor %}
      password: '{{ item.get('password', '') }}'

      {% if item.ext_meta is defined or item.labels is defined %}ext_meta:
      {%- if item.ext_meta is defined %}
      {%- for key, value in item.ext_meta.items() %}
        {{ "-" if loop.first else " "  }} {{ key }}: "{{ value }}"
      {%- endfor %}
      {%- endif %}
      {%- if item.labels is defined %}
      {%- for label in item.labels %}
      {%- for key, value in label.items() %}
        {{ "-" if loop.first and not item.ext_meta is defined else " "  }} {{ key }}: "{{ value }}"
      {%- endfor %}
      {%- endfor %}
      {%- endif %}
      {%- endif %}

{% endfor %}{% endif %}
