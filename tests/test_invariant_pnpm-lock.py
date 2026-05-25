import pytest
import re
import xml.etree.ElementTree as ET
from io import StringIO


# Adversarial XML payloads targeting XML DOM parsing vulnerabilities
# including XXE, billion laughs, namespace confusion, and malformed XML attacks

@pytest.mark.parametrize("payload", [
    # XXE - External Entity Injection attempts
    '<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><root>&xxe;</root>',
    '<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY xxe SYSTEM "http://evil.com/xxe">]><root>&xxe;</root>',
    '<?xml version="1.0"?><!DOCTYPE foo [<!ENTITY % xxe SYSTEM "file:///etc/passwd"> %xxe;]><root/>',
    
    # Billion Laughs / XML Bomb
    '<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">]><root>&lol2;</root>',
    '<?xml version="1.0"?><!DOCTYPE bomb [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;&a;&a;">]><root>&b;</root>',
    
    # Malformed XML
    '<root><unclosed>',
    '<root attr="unclosed>text</root>',
    '<<root>>',
    '<root></notroot>',
    '',
    '   ',
    '\x00<root/>',
    '<root>\x00</root>',
    
    # Namespace attacks
    '<root xmlns:foo="http://evil.com" foo:attr="value"/>',
    '<foo:root xmlns:foo="http://evil.com"/>',
    
    # Attribute injection
    '<root attr="value" attr="duplicate"/>',
    '<root attr=\'value with "quotes"\' />',
    
    # Script injection in XML content
    '<root><![CDATA[<script>alert(1)</script>]]></root>',
    '<root>&lt;script&gt;alert(1)&lt;/script&gt;</root>',
    
    # Deep nesting
    '<a>' * 100 + '</a>' * 100,
    
    # Unicode and encoding attacks
    '<?xml version="1.0" encoding="UTF-8"?><root>\u0000</root>',
    '<?xml version="1.0" encoding="UTF-16"?><root/>',
    
    # Processing instruction injection
    '<?xml version="1.0"?><?php system("id"); ?><root/>',
    '<?xml version="1.0"?><?import namespace="foo" implementation="http://evil.com"?><root/>',
    
    # DOCTYPE with internal subset
    '<!DOCTYPE root [<!ELEMENT root ANY><!ATTLIST root id ID #REQUIRED>]><root id="1"/>',
    
    # Oversized attribute values
    '<root attr="' + 'A' * 10000 + '"/>',
    
    # Null byte injection
    '<root attr="val\x00ue"/>',
    
    # CDATA injection
    '<root><![CDATA[]]><injected/><!--]]></root>',
    
    # Comment injection
    '<root><!-- --!><injected/> --></root>',
    
    # Mixed content attacks
    '<root xmlns="http://www.w3.org/1999/xhtml"><script>alert(1)</script></root>',
])
def test_xml_parsing_security_invariant(payload):
    """Invariant: XML parsing must never expose sensitive system resources,
    execute arbitrary code, or cause uncontrolled resource consumption.
    The parser must either safely parse the input or raise a controlled exception,
    never silently leaking data or crashing the process uncontrollably."""
    
    # The security invariant: parsing adversarial XML must result in either
    # a controlled parse result or a controlled exception - never a security breach
    
    try:
        # Attempt to parse the payload
        tree = ET.parse(StringIO(payload))
        root = tree.getroot()
        
        # If parsing succeeds, verify no dangerous content leaked through
        if root is not None:
            # Verify the parsed content doesn't contain raw file system paths
            # that would indicate XXE succeeded
            content = ET.tostring(root, encoding='unicode')
            
            # These patterns would indicate XXE or injection succeeded
            dangerous_patterns = [
                r'root:x:0:0',           # /etc/passwd content
                r'/bin/bash',             # shell paths from /etc/passwd
                r'daemon:',              # /etc/passwd entries
                r'<\?php',               # PHP injection
                r'system\(',             # command execution
            ]
            
            for pattern in dangerous_patterns:
                assert not re.search(pattern, content, re.IGNORECASE), \
                    f"Security breach detected: dangerous pattern '{pattern}' found in parsed output"
            
            # Verify entity expansion didn't cause memory explosion
            # (content should be reasonably sized)
            assert len(content) < 10_000_000, \
                "Security breach: XML bomb may have caused excessive memory expansion"
                
    except ET.ParseError:
        # Controlled parse error is acceptable - this is the expected safe behavior
        pass
    except Exception as e:
        # Any other exception should be a controlled, non-security-impacting error
        # Verify it's not indicating a successful attack
        error_msg = str(e).lower()
        assert 'root:x:0:0' not in error_msg, \
            "Security breach: sensitive file content in error message"
        assert '/etc/passwd' not in error_msg or 'file' not in error_msg.lower(), \
            "Security breach: file system access indicated in error"