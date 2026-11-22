# Junkit test standard

Every `*_test.go` file in Junkit follows the rules below. The goal: a reader understands what a
file verifies without opening the production code.

## 1. File layout

1. `package` clause.
2. Header block (Test Case Overview, or Test Helpers block for helper-only files).
3. Imports.
4. Shared constants, fixtures and fakes used by several tests in the file.
5. Helper functions (`t.Helper()` first line).
6. Test functions, grouped by the function or type under test.
7. Benchmarks and fuzz tests last.

Black-box tests live in `package <name>_test`. Tests that need unexported identifiers live in
`*_internal_test.go` (`package <name>`). `export_test.go` only re-exports unexported identifiers.

## 2. Test Case Overview

Placed right after the `package` clause. Columns are separated by at least two spaces and
aligned. One row per scenario, in source order.

```go
package security_test

// =============================================================================
// TEST CASE OVERVIEW
// =============================================================================
//
// File: token_hash_test.go
// Purpose: Verify token hashing and constant-time secret comparison.
// Type: Unit
//
// Test functions: 2
// Total scenarios: 3
//
// #  Test Case                           Scenario           Expected Result
// 1  TestHashToken_ReturnsSHA256Hex      Known input "abc"  Returns the known SHA-256 hex digest
// 2  TestHashToken_ReturnsSHA256Hex      Random token       Returns 64 lowercase hex characters
// 3  TestSecureEqual_ComparesSecrets     Equal strings      Returns true
//
// =============================================================================
```

- `Type:` is `Unit` or `Integration (<what it needs, e.g. REDIS_TEST_ADDR>)`. A file mixing both
  says `Unit + Integration` and names the integration tests.
- Benchmarks and fuzz tests are **not** scenarios. They get their own lines after the table:

  ```go
  // Fuzz tests: 1
  //   FuzzTokenSealerOpen  Random token strings  Never panics, never accepts a forged token
  ```

- Helper-only files (no `Test` functions) use a `TEST HELPERS` block instead, with `File`,
  `Purpose` and `Used by` lines. They never get a fake overview.

## 3. Counting

- **Test functions** = top-level `func TestXxx(t *testing.T)`.
- **Scenario** = one leaf test run, i.e. what `go test -v` reports as a test that has no
  subtests. A test without `t.Run` is one scenario. A table with five cases is five scenarios.
  Subtests that only group other subtests are not counted.
- Scenarios whose count depends on the environment (for example an integration test that skips
  before running its subtests) are listed as they run when the environment is present, and the
  row says so.

## 4. Naming

- Top-level: `Test<Subject>_<Behavior>`, e.g. `TestClientKey_GroupsIPv6ByPrefix`,
  `TestNewTokenSealer_RejectsInvalidKeys`. For a method, the subject may be `<Type>_<Method>`:
  `TestLocalService_UploadFile_ValidatesExtension`. Never `TestSuccess`, `TestError`, `TestCase1`.
- Subtests and table cases: short lower-case phrases describing the scenario, e.g.
  `"ipv6 address is grouped by /64"`. Not raw input values when the input alone does not explain
  the scenario.
- Table slices use `tests` and the loop variable `tt`. Use slices, not maps, so order is stable.

## 5. Body

- Arrange, Act, Assert, in that order. Add the three comments only when the test is long enough
  that they help.
- Each scenario states its input and expected result explicitly (`want`, `wantErr`, ...).
- Assertion messages follow `Func(input) = got, want want`. One condition per assertion, so a
  failure says exactly what was wrong.
- Never ignore an error from setup: `if err != nil { t.Fatalf("setup: %v", err) }`.
- Use `t.Fatalf` when continuing makes no sense, `t.Errorf` otherwise.
- Use a table only when cases share setup, action and assertions. Otherwise write separate tests.

## 6. Helpers, fakes and isolation

- Extract a helper only when it removes real noise and its name says what it does.
- Fakes are small hand-written types in the test file that uses them (or a `*_helper_test.go`
  file when several files share them). No mocking frameworks.
- Clean up with `t.Cleanup`. Use `t.TempDir`, `t.Setenv`, `httptest`. No test depends on another
  test running first.
- Integration tests skip with a clear message when their environment is missing.

## 7. Keeping overviews correct

When adding, removing or renaming a test or a table case, update the overview rows and both
counts in the same change. `go test -v ./<pkg> -run '<TestName>'` lists the leaf runs to compare.

CI (`.github/workflows/test.yml`) runs on every push and pull request: `gofmt`, `go vet`,
`go test -race`, then `.github/scripts/check_test_overview.py`, which fails the build when a test
fails or an overview disagrees with the tests that ran. Run the same check locally:

```sh
go test -count=1 -race -json ./... > test.json; python3 .github/scripts/check_test_overview.py test.json
```
