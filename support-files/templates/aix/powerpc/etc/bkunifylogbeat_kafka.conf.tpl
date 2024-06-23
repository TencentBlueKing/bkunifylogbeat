{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: kafka
      hosts: {%for host in item.hosts %}
        - {{ host }}
        {% endfor %}

      topics: item.get('topics', [])

      {% if item.ssl is defined and item.ssl %}ssl: {{ item.ssl }}{% endif %}
      username: '{{ item.get('username', '') }}'
      password: '{{ item.get('password', '') }}'

      group_id: '{% if item.group_id is defined and item.group_id %}{{ item.get('group_id') }}{% else %}bkunifylogbeat_{{ dataid | int }}{% endif %}'

      delimiter: '{{ item.get('delimiter', '') }}'
      {% if item.filters is defined and item.filters %}filters:{% for filter in item.filters %}
       - conditions:{% for condition in filter.conditions %}
        - index: {{ condition.index | int }}
         key: '{{ condition.key}}'
         op: '{{ condition.op }}'{% endfor %}
      {% endfor %}{% endif %}

      initial_offset: '{{ item.get('initial_offset', 'newest') }}'


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
