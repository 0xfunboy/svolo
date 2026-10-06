---
name: pr-review
description: Review an authorized GitHub pull request without changing the user's checkout or publishing a review unless explicitly requested.
---

# Pull request review

Resolve repository, host, account and PR number from the user or the selected project.
Read the PR metadata, diff, checks and relevant discussion. Use the configured GitHub
CLI account; do not switch the global active account simply to bypass an access error.
Do not place authentication tokens in command arguments or logs.

```bash
gh pr view <number> --repo <owner/repository> --json number,title,body,baseRefName,headRefName,headRefOid,files,commits,statusCheckRollup
gh pr diff <number> --repo <owner/repository>
```

Review correctness, regressions, input validation, authorization, data integrity and
missing tests. Give concrete findings with file paths, affected lines, trigger conditions
and expected impact. Distinguish confirmed defects from questions and style preferences.

Do not run repository-controlled code just to read a diff. Tests require an approved
isolated checkout and explicit execution scope. Preserve unrelated files and worktrees.
Do not checkout over the user's changes, post comments, approve, merge or push without
separate authority. State when required context or checks were unavailable.
