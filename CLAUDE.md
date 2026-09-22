# Comments

**HARD RULE.** Write a comment (or keep an existing one) ONLY if all 3 hold:
1. it explains the code written just below it
2. that code is not clear to read on its own
3. the info is necessary to understand that code

Everything else gets deleted: usage-context ("called by X" — wrong place), rationale/history/design notes, godoc restating the symbol name, narration of the next line. If only part of a comment qualifies, trim to that fragment. Keep compiler/lint directives (`//go:`, nolint).
