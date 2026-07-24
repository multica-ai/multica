# Python

Inspect `pyproject.toml`, `pytest.ini`, `tox.ini`, `setup.cfg`, and repository
scripts to identify the environment wrapper and runner. Preserve repository
wrappers such as tox, nox, uv, or Poetry when they establish dependencies or
environment variables.

For direct pytest usage, a file target is:

```text
["python", "-m", "pytest", "path/to/test_file.py"]
```

A single case uses its pytest node id:

```text
["python", "-m", "pytest", "path/to/test_file.py::TestClass::test_name"]
```

Verify the node id before executing:

```text
["python", "-m", "pytest", "--collect-only", "-q", "path/to/test_file.py::TestClass::test_name"]
```

Parameterized cases add their generated id in brackets; copy it from collection
output instead of guessing it. Do not assume a unittest, Django, or custom
runner accepts pytest selectors; use the detected runner's own syntax.

## Official documentation

- pytest invocation, node ids, `python -m pytest`, and `--collect-only`:
  https://docs.pytest.org/en/stable/how-to/usage.html
