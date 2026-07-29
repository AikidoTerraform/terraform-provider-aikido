# Security Policy

## Reporting Security Vulnerabilities

At Aikido Security, we take security seriously. If you discover a security vulnerability in this Terraform provider, please report it responsibly.

**Please do NOT report security vulnerabilities through public GitHub issues.**

### How to Report

Please report security vulnerabilities by emailing: **security@aikido.dev**

Include the following information:

- Description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Any suggested fixes (if available)

### What to Expect

- We will acknowledge receipt of your vulnerability report within 48 hours
- We will provide a more detailed response within 5 business days
- We will work with you to understand and validate the issue
- Once validated, we will work on a fix and coordinate disclosure timing

## Supported Versions

We provide security updates for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < 0.1   | :x:                |

## Security Best Practices

When using this provider:

1. **Protect your API credentials**: Never commit your Aikido `client_id` or `client_secret` to version control.
2. **Prefer environment variables**: Supply credentials through `AIKIDO_CLIENT_ID` and `AIKIDO_CLIENT_SECRET` rather than hardcoding them in `.tf` files.
3. **Use variable files carefully**: If you store credentials in `terraform.tfvars`, add that file to `.gitignore`.
4. **Protect Terraform state**: State can contain sensitive values. Use a secure, access-controlled backend and avoid committing state to version control.
5. **Redact debug output**: Never paste raw `TF_LOG=DEBUG` output into public issues, as it can expose the `client_secret`, OAuth tokens, or `Authorization` headers. Redact secrets first.
6. **Rotate credentials**: Periodically rotate the client secret through the [Aikido REST API integration page](https://app.aikido.dev/settings/integrations/api/aikido/rest).

## Third-Party Dependencies

This provider depends on the Terraform Plugin Framework and the Aikido management API. Keep your Terraform CLI and provider versions up to date to receive security patches.

## Additional Resources

- [Aikido Security Documentation](https://help.aikido.dev/)
- [Aikido Management API Documentation](https://apidocs.aikido.dev/)
- [Terraform Sensitive Variables](https://developer.hashicorp.com/terraform/tutorials/configuration-language/sensitive-variables)
