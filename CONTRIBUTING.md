# Contributing

Thank you for your interest in contributing to the Aikido Terraform provider.

## Code of Conduct

This project follows our [Code of Conduct](CODE_OF_CONDUCT.md), adapted from the Contributor Covenant. We also expect participants to respect the [HashiCorp Community Guidelines](https://www.hashicorp.com/community-guidelines). Please read both before participating.

## Reporting Security Issues

Do not report security vulnerabilities through public GitHub issues. See our [Security Policy](SECURITY.md) for how to report them privately.

## Reporting Bugs

Use the [bug report issue template](https://github.com/AikidoTerraform/terraform-provider-aikido/issues/new?template=bug-report.md) and include:

- Terraform and provider versions
- A minimal reproducible configuration
- Expected vs. actual behavior
- Relevant debug output (`TF_LOG=DEBUG`) when possible

## Development Setup

See [`DEVELOPMENT.md`](DEVELOPMENT.md) for prerequisites, building the provider, local Terraform e2e testing, and available `make` targets.

Quick checks before opening a pull request:

```shell
make fmt
make lint
make test
make generate   # if you changed schemas or provider metadata
```

CI runs the same steps on every pull request (build, unit tests, lint, and docs verification).

## AI Use Policy and Guidelines

Our goal in this project is to develop a stable and well-maintained provider library. This requires careful attention to detail in every change we integrate. Maintainer time and attention is very limited, so it's important that changes you ask us to review represent your best work.

You are encouraged to use tools that help you write good code, including AI tools. However, you always need to understand and explain the changes you're proposing to make, whether or not you used an LLM as part of your process to produce them. The answer to "Why did you make change X?" should never be "I'm not sure. The AI did it."

Do not submit an AI-generated PR you haven't personally understood and tested, as this wastes maintainers' time. PRs that appear to violate this guideline will be closed without review. If you do submit a largely AI-generated PR, clearly mark it as such in the description. Note that maintainers may still close it without further review if it does not seem worthwhile.

### Using AI as a Coding Assistant

- Don't skip becoming familiar with the part of the codebase you're working on. This will let you write better prompts and validate their output if you use an LLM. Code assistants can be a useful search engine/discovery tool in this process, but don't trust claims they make about how Terraform, Terraform providers, or the Aikido API works. LLMs are often wrong, even about details that are clearly answered in Terraform Plugin Framework documentation, Terraform documentation, or [Aikido API documentation](https://apidocs.aikido.dev/).
- Split up your changes into coherent commits, even if an LLM generates them all in one go. This makes it easier for maintainers to review and understand your changes, and also helps you keep track of your own work.
- Don't simply ask an LLM to add code comments, as it will likely produce a bunch of text that unnecessarily explains what's already clear from the code. If using an LLM to generate comments, be really specific in your request, demand succinctness, and carefully edit the result.

### Using AI for Communication

Contributors are expected to communicate with intention, to avoid wasting maintainer time with long, sloppy writing. We strongly prefer clear and concise communication about points that actually require discussion over long AI-generated comments.

When you use an LLM to write a message for you, it remains your responsibility to read through the whole thing and make sure that it makes sense to you and represents your ideas concisely. A good rule of thumb is that if you can't make yourself carefully read some LLM output that you generated, nobody else wants to read it either.

Here are some concrete guidelines for using LLMs as part of your communication workflows:

- When writing a pull request description, do not include anything that's obvious from looking at your changes directly (e.g., files changed, functions updated, etc.). Instead, focus on the why behind your changes. Don't ask an LLM to generate a PR description on your behalf based on your code changes, as it will simply regurgitate the information that's already there.
- Similarly, when responding to a pull request comment, explain your reasoning. Don't prompt an LLM to re-describe what can already be seen from the code.
- Verify that everything you write is accurate, whether or not an LLM generated any part of it. The maintainers will be unable to review your contributions if you misrepresent your work (e.g., wrongly describing your code changes, their effect, or your testing process).
- Complete all parts of the PR description template, including the checklists. Don't simply overwrite the template with LLM output.
- Clarity and succinctness are much more important than perfect grammar, so you shouldn't feel obliged to pass your writing through an LLM. If you do ask an LLM to clean up your writing style, be sure it does not make it longer in the process. Demand succinctness in your prompt.
- Quoting an LLM answer is usually less helpful than linking to relevant primary sources, like source code, or reference materials. If you do need to quote an LLM answer in a discussion, clearly distinguish LLM output from your own thoughts.

## Pull Requests

1. Fork the repository and create a branch from `main`.
2. Make focused changes with clear commit messages.
3. Add or update unit tests for behavior changes. Prefer mocked HTTP tests in `internal/`; they do not require Aikido credentials.
4. Regenerate documentation when resource or provider schemas change:

   ```shell
   make generate
   ```

5. Update [`CHANGELOG.md`](CHANGELOG.md) for user-visible changes (new resources, bug fixes, breaking changes). Follow the existing format under the unreleased section.
6. Open a pull request describing what changed and why. Link related issues when applicable.

## Acceptance Tests

Acceptance tests (`TF_ACC=1`) call the live Aikido API and require OAuth2 credentials.

To run them locally:

```shell
export AIKIDO_CLIENT_ID="..."
export AIKIDO_CLIENT_SECRET="..."
make testacc
```

Use a disposable Aikido workspace—not production customer data.

## License

By contributing, you agree that your contributions will be licensed under the [Mozilla Public License 2.0](https://opensource.org/licenses/MPL-2.0), the same license as this project.
