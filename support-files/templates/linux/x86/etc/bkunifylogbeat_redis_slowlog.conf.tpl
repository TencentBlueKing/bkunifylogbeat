{# Go YAML rejects the UTF-16 surrogate pairs produced by tojson for non-BMP characters. #}
{# NodeMan renders context strings before this template; producers must neutralize literal Jinja syntax. #}
{% macro yaml_string(value) -%}
"
{%- for char in value|string -%}
{%- if '\ud800' <= char <= '\udfff' or char in '\ufffe\uffff' -%}
{{ '\ufffd' }}
{%- else -%}
{%- set encoded = char|tojson -%}
{%- if encoded|length > 8 -%}{{ char }}{%- else -%}{{ encoded[1:-1] }}{%- endif -%}
{%- endif -%}
{%- endfor -%}
"
{%- endmacro %}

{% if local is defined %}
local: {% for item in local %}
    - dataid: {{ dataid | int }}
      type: redis
      hosts: {%for host in item.hosts %}
        - '{{ host }}'{% endfor %}
      password: '{{ item.get('password', '') }}'
      password_file: '{{ item.get('password_file', '') }}'
      idle_timeout: '{{ item.get('idle_timeout', 10) | int }}s'
      maxconn: {{ item.get('maxconn', 10) | int }}

      {% if item.ext_meta is defined or item.labels is defined %}ext_meta:
      {%- if item.ext_meta is defined %}
      {%- for key, value in item.ext_meta.items() %}
        {{ yaml_string(key) }}: {{ yaml_string(value) }}
      {%- endfor %}
      {%- endif %}
      {%- if item.labels is defined %}
      {%- for label in item.labels %}
      {%- for key, value in label.items() %}
        {{ yaml_string(key) }}: {{ yaml_string(value) }}
      {%- endfor %}
      {%- endfor %}
      {%- endif %}
      {%- endif %}

      {% if item.output is defined and item.output %}{{ item.output.type }}: {{ item.output.params }}{% endif %}

{% endfor %}{% endif %}
