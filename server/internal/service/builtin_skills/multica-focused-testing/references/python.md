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

Use pytest collection output when available to verify the node ids before
executing. Do not assume a unittest, Django, or custom runner accepts pytest
selectors; use the detected runner's own syntax.
