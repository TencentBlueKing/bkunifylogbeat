{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: redis
      hosts: {%for host in item.hosts %}
        - '{{ host }}'{% endfor %}
      password: '{{ item.get('password', '') }}'

      {% if item.ext_meta is defined %}ext_meta: {{ item.get('ext_meta') }}{% endif %}
{% endfor %}{% endif %}
