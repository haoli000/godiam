# Test Certificates

This directory contains a script to generate self-signed certificates for
development and testing. **Do not use these certificates in production.**

## Generate

```bash
./generate.sh
```

This creates `ca.crt`, `ca.key`, `server.crt`, and `server.key` in this
directory. The generated files are git-ignored.
