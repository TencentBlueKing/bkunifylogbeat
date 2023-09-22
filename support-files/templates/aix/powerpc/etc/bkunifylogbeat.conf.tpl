{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      paths:{% for path in item.paths %}
        - '{{ path }}'{% endfor %}
      {% if item.exclude_files is defined and item.exclude_files %}
      exclude_files:{% for ext in item.exclude_files %}
        - '{{ ext }}'{% endfor %}{% endif %}
      tail_files: {{ item.get('tail_files',  'true') | lower }}
      encoding: '{{ item.get('encoding', 'utf-8') | lower }}'
      delimiter: '{{ item.get('delimiter', '') }}'
      {% if item.filters is defined and item.filters %}filters:{% for filter in item.filters %}
       - conditions:{% for condition in filter.conditions %}
         - index: {{ condition.index | int }}
           key: '{{ condition.key}}'
           op: '{{ condition.op }}'{% endfor %}
      {% endfor %}{% endif %}
      package: {{ item.get('package',  'true') | lower }}
      package_count: {{ item.get('package_count', 10) | int }}
      output_format: '{{ item.get('output_format',  'v2') | lower }}'
      {% if item.multiline_pattern is defined %}
      multiline.pattern: '{{ item['multiline_pattern'] }}'
      multiline.max_lines: '{{ item.get('multiline_max_lines', 500) | int }}'
      multiline.timeout: '{{ item.get('multiline_timeout', 2) | int }}s'
      multiline.negate: true
      multiline.match: after
      {% endif %}

      scan_frequency: '{{ item.get('scan_frequency', 10) | int }}s'
      close_inactive: '{{ item.get('close_inactive', 120) | int }}s'
      clean_removed: '{{ item.get('clean_removed',  'true') | lower }}'
      {% if item.harvester_limit is defined %}harvester_limit: {{ item['harvester_limit'] | int }}{% endif %}
      {% if item.ignore_older is defined %}ignore_older: '{{ item['ignore_older'] | int }}s'{% endif %}
      {% if item.clean_inactive is defined %}clean_inactive: '{{ item['clean_inactive'] | int }}s'{% endif %}
      {% if item.max_bytes is defined %}max_bytes: {{ item['max_bytes'] | int }}{% endif %}

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

{% endfor %}{% endif %}
