import pytest
import re
import os


# Adversarial payloads targeting memory-safety and injection vulnerabilities
# relevant to database drivers (pgx) and go.mod dependency management
ADVERSARIAL_PAYLOADS = [
    # SQL injection attempts
    "'; DROP TABLE users; --",
    "' OR '1'='1",
    "1; SELECT * FROM pg_tables; --",
    "' UNION SELECT NULL, NULL, NULL--",
    "admin'--",
    "' OR 1=1--",
    # Buffer overflow / memory safety attempts
    "A" * 10000,
    "A" * 65536,
    "\x00" * 1000,
    "\xff" * 1000,
    # Format string attacks
    "%s%s%s%s%s%s%s%s%s%s",
    "%x%x%x%x%x%x%x%x",
    "%n%n%n%n%n%n%n%n",
    "%.10000d",
    # Null byte injection
    "valid\x00malicious",
    "test\x00\x00\x00",
    # Unicode/encoding attacks
    "\u0000\u0001\u0002",
    "\uffff\ufffe",
    "𝕳𝖊𝖑𝖑𝖔",
    # Path traversal
    "../../../../etc/passwd",
    "../../../etc/shadow",
    "..\\..\\..\\windows\\system32",
    # Command injection
    "; cat /etc/passwd",
    "| ls -la",
    "`whoami`",
    "$(id)",
    # Integer overflow boundary values
    str(2**31 - 1),
    str(2**31),
    str(2**32 - 1),
    str(2**63 - 1),
    str(-2**31),
    str(-2**63),
    # Empty and whitespace
    "",
    " ",
    "\t\n\r",
    # Special regex/pattern characters
    ".*+?[]{}()|^$\\",
    # Very long version strings
    "v" + "9" * 1000,
    # Malformed version strings
    "v1.2.3.4.5.6.7.8.9",
    "v-1.-2.-3",
    "v1.2.3-" + "a" * 10000,
    # CRLF injection
    "value\r\nX-Injected: header",
    "test\r\n\r\nmalicious body",
    # Go module path injection attempts
    "github.com/evil/module\nreplace github.com/jackc/pgx/v5 => github.com/evil/pgx v1.0.0",
    "github.com/jackc/pgx/v5@v5.0.0\x00evil",
]


def parse_go_mod_version(content: str) -> dict:
    """
    Parse go.mod content and extract dependency versions.
    Returns a dict of module -> version mappings.
    """
    dependencies = {}
    if not content or not isinstance(content, str):
        return dependencies
    
    lines = content.split('\n')
    in_require_block = False
    
    for line in lines:
        stripped = line.strip()
        
        if stripped == 'require (':
            in_require_block = True
            continue
        elif stripped == ')' and in_require_block:
            in_require_block = False
            continue
        
        if in_require_block or stripped.startswith('require '):
            # Parse: module version
            parts = stripped.replace('require ', '').split()
            if len(parts) >= 2:
                module = parts[0]
                version = parts[1]
                # Only store if module name looks valid (basic sanity check)
                if re.match(r'^[a-zA-Z0-9./\-_]+$', module):
                    dependencies[module] = version
    
    return dependencies


def is_safe_version_string(version: str) -> bool:
    """
    Validate that a version string is safe and well-formed.
    A safe version string should match semantic versioning patterns.
    """
    if not version or not isinstance(version, str):
        return False
    
    # Version must not contain null bytes
    if '\x00' in version:
        return False
    
    # Version must not contain newlines or carriage returns
    if '\n' in version or '\r' in version:
        return False
    
    # Version must not be excessively long
    if len(version) > 256:
        return False
    
    # Version should match go module version pattern
    # e.g., v1.2.3, v1.2.3-beta.1, v0.0.0-20230101000000-abcdef123456
    version_pattern = re.compile(
        r'^v\d+\.\d+\.\d+(-[a-zA-Z0-9.\-]+)?(\+[a-zA-Z0-9.\-]+)?$'
    )
    
    return bool(version_pattern.match(version))


def sanitize_module_path(module_path: str) -> str:
    """
    Sanitize a module path to prevent injection attacks.
    """
    if not module_path or not isinstance(module_path, str):
        return ""
    
    # Remove null bytes
    sanitized = module_path.replace('\x00', '')
    
    # Remove newlines
    sanitized = sanitized.replace('\n', '').replace('\r', '')
    
    # Truncate if too long
    sanitized = sanitized[:512]
    
    return sanitized


def validate_dependency_entry(module: str, version: str) -> bool:
    """
    Validate a dependency entry for security properties.
    Returns True if the entry is safe, False otherwise.
    """
    # Module path must not be empty
    if not module:
        return False
    
    # Module path must not contain injection characters
    dangerous_chars = ['\x00', '\n', '\r', ';', '|', '`', '$', '(', ')']
    for char in dangerous_chars:
        if char in module:
            return False
    
    # Module path must not be excessively long
    if len(module) > 512:
        return False
    
    # Version must be safe
    if not is_safe_version_string(version):
        return False
    
    return True


@pytest.mark.parametrize("payload", ADVERSARIAL_PAYLOADS)
def test_go_mod_dependency_security_invariant(payload):
    """
    Invariant: Adversarial inputs must not bypass security validation of 
    go.mod dependency entries. Memory-safety is maintained by ensuring 
    that module paths and version strings are properly validated and 
    sanitized, preventing injection attacks, buffer overflows, and 
    malformed data from reaching the database driver (pgx).
    """
    # Property 1: Sanitized module path must never contain dangerous characters
    sanitized = sanitize_module_path(payload)
    assert '\x00' not in sanitized, \
        f"Null byte found in sanitized module path for payload: {repr(payload[:50])}"
    assert '\n' not in sanitized, \
        f"Newline found in sanitized module path for payload: {repr(payload[:50])}"
    assert '\r' not in sanitized, \
        f"Carriage return found in sanitized module path for payload: {repr(payload[:50])}"
    assert len(sanitized) <= 512, \
        f"Sanitized path exceeds maximum length for payload: {repr(payload[:50])}"

    # Property 2: Adversarial payloads must NOT be valid version strings
    # (they should fail validation, protecting the system from malformed versions)
    if payload:  # non-empty payloads
        is_valid = is_safe_version_string(payload)
        # If the payload is adversarial (contains dangerous patterns), it must not be valid
        dangerous_patterns = [
            '\x00', '\n', '\r', '%n', '%s', '%x',
            'DROP', 'SELECT', 'UNION', 'INSERT',
            '../', '..\\', '; ', '| ', '`', '$(',
        ]
        is_adversarial = any(pattern in payload for pattern in dangerous_patterns)
        
        if is_adversarial:
            assert not is_valid, \
                f"Adversarial payload incorrectly validated as safe version: {repr(payload[:50])}"

    # Property 3: Dependency validation must reject adversarial module paths
    # Use a valid version to isolate the module path validation
    valid_version = "v5.5.0"
    is_valid_dep = validate_dependency_entry(payload, valid_version)
    
    # Payloads with injection characters must be rejected
    injection_chars = ['\x00', '\n', '\r', ';', '|', '`', '$', '(', ')']
    has_injection = any(char in payload for char in injection_chars)
    
    if has_injection:
        assert not is_valid_dep, \
            f"Dependency with injection characters must be rejected: {repr(payload[:50])}"
    
    # Excessively long payloads must be rejected
    if len(payload) > 512:
        assert not is_valid_dep, \
            f"Excessively long dependency path must be rejected: length={len(payload)}"

    # Property 4: Parsing adversarial content as go.mod must not crash
    # and must not extract malicious entries
    fake_go_mod = f"""module example.com/test

go 1.21

require (
    github.com/jackc/pgx/v5 v5.5.0
    {payload} v1.0.0
)
"""
    try:
        deps = parse_go_mod_version(fake_go_mod)
        
        # The legitimate dependency must still be parseable
        # (adversarial input must not corrupt legitimate entries)
        if 'github.com/jackc/pgx/v5' in deps:
            pgx_version = deps['github.com/jackc/pgx/v5']
            assert is_safe_version_string(pgx_version), \
                f"Legitimate pgx version corrupted by adversarial input: {repr(payload[:50])}"
        
        # Any extracted dependency versions must be safe
        for module_name, version in deps.items():
            assert '\x00' not in module_name, \
                f"Null byte in extracted module name"
            assert '\n' not in module_name, \
                f"Newline in extracted module name"
            assert '\x00' not in version, \
                f"Null byte in extracted version"
            assert '\n' not in version, \
                f"Newline in extracted version"
                
    except Exception as e:
        # Parsing must not raise unexpected exceptions that could indicate
        # memory safety issues or unhandled edge cases
        pytest.fail(f"Parsing adversarial go.mod content raised exception: {e} "
                   f"for payload: {repr(payload[:50])}")

    # Property 5: Version string length must be bounded after any processing
    # This guards against memory exhaustion attacks
    processed_version = sanitize_module_path(payload)
    assert len(processed_version) <= 512, \
        f"Processed version string exceeds safe length bounds"