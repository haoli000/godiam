# rt_rewrite Extension

## Overview

The `rt_rewrite` extension provides AVP (Attribute Value Pair) rewriting capabilities for godiam, enabling DRA (Diameter Routing Agent) functionality. This extension allows you to:

- **Map** AVP data from one AVP to another (copy values)
- **Drop** AVPs from messages (remove them)

This is useful for:
- Protocol translation between different Diameter versions
- Privacy filtering (removing sensitive AVPs)
- Message enrichment (adding routing/forwarding information)
- Interoperability between different vendor implementations

## Features

- ✅ Simple AVP to AVP mapping
- ✅ Nested/grouped AVP support
- ✅ AVP dropping
- ✅ Automatic creation of intermediate grouped AVPs
- ✅ Type validation with compatibility checks
- ✅ Dictionary-based AVP lookup
- ✅ Runtime configuration (via YAML config)
- ✅ Priority-based routing handler integration

## Configuration

Add to your `diameter.yaml`:

```yaml
extensions:
  - name: "rt_rewrite"
    config:
      rules:
        # MAP: Copy Origin-Host to Destination-Host
        - map:
            source: "Origin-Host"
            destination: "Destination-Host"

        # DROP: Remove sensitive AVP
        - drop: "User-Password"
```

### Map Rules

Copy data from source AVP to destination AVP:

```yaml
- map:
    source: "Source-AVP"
    destination: "Destination-AVP"
```

### Nested AVP Mapping

For grouped AVPs, use colon (`:`) to specify path:

```yaml
- map:
    source: "Session-Id"
    destination: "Proxy-Info:Session-Id"
```

### Drop Rules

Remove AVPs from messages:

```yaml
- drop: "AVP-Name"
```

Or remove from grouped AVPs:

```yaml
- drop: "Proxy-Info:Redirect-Host"
```

## Examples

### DRA: Protocol Translation

Map between different AVPs for protocol compatibility:

```yaml
rules:
  - map:
      source: "User-Name"
      destination: "MSISDN"
```

### DRA: Privacy Filtering

Remove sensitive information:

```yaml
rules:
  - drop: "User-Password"
  - drop: "CHAP-Password"
  - drop: "Digest-Realm"
```

### DRA: Message Enrichment

Add routing information:

```yaml
rules:
  - map:
      source: "Origin-Realm"
      destination: "Proxy-Info:Origin-Realm"
  - map:
      source: "Origin-Host"
      destination: "Proxy-Info:Origin-Host"
```

### DRA: 3GPP Specific

Handle 3GPP-specific AVPs:

```yaml
rules:
  - map:
      source: "Subscription-Data"
      destination: "Requested-Subscription-Data"
  - drop: "Supported-Features"
```

## Type Compatibility

AVP type checking ensures data integrity:

| Source Type | Destination Type | Status |
|-------------|------------------|---------|
| OctetString | Any | ✅ Compatible |
| Integer32 | Integer32 | ✅ Compatible |
| UTF8String | UTF8String | ✅ Compatible |
| Integer32 | UTF8String | ⚠️ Warning (logged) |
| Grouped | Grouped | ✅ Compatible (copies children) |
| Simple | Grouped | ❌ Not supported |

## Integration

The extension registers as a routing handler with priority 40 (higher than `app_redirect`'s 50), ensuring AVP rewrites happen before routing decisions.

## Comparison with freeDiameter

| Feature | freeDiameter rt_rewrite | godiam rt_rewrite |
|---------|----------------------|------------------------|
| Config format | C-style syntax | YAML |
| MAP syntax | `MAP = "src" > "dest";` | `source: "src", destination: "dest"` |
| DROP syntax | `DROP = "avp";` | `drop: "avp"` |
| Nested AVPs | `:` separator | `:` separator (same) |
| Reload | SIGUSR1 signal | Config reload on restart |
| Type checking | At load time | At load time |

## Testing

To test the rt_rewrite extension:

1. Create a test configuration:
```yaml
identity: "dra.example.com"
realm: "example.com"

extensions:
  - name: "rt_rewrite"
    config:
      rules:
        - map:
            source: "Origin-Host"
            destination: "Destination-Host"
        - drop: "Product-Name"
```

2. Start the daemon:
```bash
./diameterd --config examples/configs/dra_config.yaml
```

3. Send a test message and verify:
- Origin-Host value is copied to Destination-Host
- Product-Name AVP is removed from the message

## Limitations

1. **No wildcard matching**: AVP names must match exactly
2. **No regex support**: Cannot match AVP patterns
3. **Single match per rule**: Only first occurrence of an AVP is matched
4. **No value-based matching**: Cannot match AVPs by their values
5. **No grouped AVP copy**: Cannot copy entire grouped AVP as a unit

## Performance

- AVP dictionary lookups are cached
- Minimal overhead per message (rule application)
- Suitable for high-throughput DRA deployments

## Security Considerations

- Validate all MAP rules to prevent data leaks
- Be careful with DROP rules to not break protocol compliance
- Monitor logs for type mismatch warnings
- Test thoroughly before production deployment

## Troubleshooting

### AVP not found
```
rt_rewrite: Invalid AVP path 'Unknown-AVP' in DROP rule at index 0
```
**Solution**: Check AVP name spelling and dictionary includes this AVP

### Type mismatch warning
```
rt_rewrite: Type mismatch between 'Integer32-AVP' and 'UTF8String-AVP' (continuing anyway)
```
**Solution**: Review and fix the rule to use compatible types

### Rule not applied
**Solution**: Check:
- AVP name spelling
- Rule syntax (indentation)
- Extension is loaded (check logs)

## License

Same as godiam project license.
