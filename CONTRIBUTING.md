# Contributing Guidelines

## Commit Message Rules (Git Flow & Changelog)

1. **Allowed commit types (for changelog):**
   - `new`: for new features
   - `chg`: for changes/improvements
   - `fix`: for bug fixes

2. **Format:**
   - `type: short summary`
   - Example: `new: add OAuth2 login`


3. **Work in Progress (WIP):**
   - If a commit is a work in progress, include `WIP` in the headline (e.g., `WIP new: add OAuth2 login`).

4. **Body (optional):**
   - Use to explain “why” and “how” if not obvious.

5. **Reference issues/PRs if relevant:**
   - Example: `Closes #123`

6. **No generic messages like “update”.**

7. **Each commit should be atomic and focused.**

8. **Follow Git Flow:**
   - Use `feature/`, `bugfix/`, `hotfix/`, `release/` branches as appropriate.
   - Merge via PRs, not direct to main.

9.  **Changelog generation:**
    - Only commits with `new`, `chg`, or `fix` will appear in the changelog.

---

Thank you for contributing!
