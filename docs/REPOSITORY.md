# Public source repository

The public repository is [0xfunboy/svolo on GitHub](https://github.com/0xfunboy/svolo).

```bash
git clone https://github.com/0xfunboy/svolo.git
```

Clone and fetch work without signing in. Publishing changes requires authorized GitHub credentials, kept outside the repository. Application accounts and their provider logins are stored separately from Git.

`.gitignore` excludes runtime databases, authentication/configuration files, browser profiles, credentials, build outputs and dependencies. Publication checks compare the source against actual private credential values and run a redacted secret scanner. Review staged files before future commits: ignoring a file does not remove a secret that was already committed. Verification reports containing local deployment details stay outside the public checkout.

The project license is preserved. Public source publication does not qualify a stable production release; the current development status and release requirements remain in effect.
