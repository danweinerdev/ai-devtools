# Tests that prove described behaviour

A test proves a behaviour the PRD describes, not a line of code. The proof is that each described behaviour has a check that was seen to fail for the right reason (aid-make-verify).

## Principles

### 1. Prove a described behaviour

The test's name and assertions come from a behaviour the PRD describes. A test that pins an internal detail will fail on a harmless refactor and pass when the behaviour breaks.

```python
# bad: pins an internal detail
assert service._cache is cache

# good: the PRD says repeated lookups reuse the cache
service.lookup("u1")
service.lookup("u1")
assert gateway.calls == 1
```

### 2. Be independent of other tests and run order

Each test arranges its own state. A test that reads what another test wrote passes or fails by run order, and hides the failure when the suite is filtered.

```python
# bad: depends on test_saves_user running first
state = {}
def test_saves_user(): state["user"] = create_user("ada")
def test_reads_user():  assert state["user"].name == "ada"

# good: every test builds what it needs
def test_reads_user():
    store = empty_store()
    store.put(create_user("ada"))
    assert store.get("ada").name == "ada"
```

### 3. Test one concept, and name it for the behaviour

A test with several unrelated assertions fails for whichever reason comes first and doesn't say which behaviour broke. Several assertions on one concept are fine; that is still one test.

```python
# bad: a vague name and two unrelated concepts
def test_add():
    assert add(1, 2) == 3
    assert add(0, 0) == 0

# good: one concept per test, named for the behaviour
def test_add_sums_two_positive_numbers():
    assert add(1, 2) == 3

def test_add_treats_zero_as_the_identity():
    assert add(0, 0) == 0

# good: multiple assertions, one concept
def test_invoice_total_includes_tax():
    invoice = build_invoice([item("book", 100)])
    assert invoice.subtotal == 100
    assert invoice.total == 110
```

### 4. Exercise public interfaces and assert on values

The test goes through the interface callers use. It asserts the value that matters, not that something exists.

```python
# bad: presence only
assert result is not None
assert response.status_code

# good: the value the behaviour promises
assert result.name == "Ada"
assert response.status_code == 200
```

### 5. Mock only real system boundaries

Mock the network, a third-party SDK, the clock, randomness, or an OS resource. Never mock the code under test, and never mock a collaborator that is part of the behaviour being proved. For your own data layer, prefer a real in-memory database to a mock of it.

```python
# bad: replaces the thing being tested
repo = Mock()
repo.find.return_value = User("Ada")
assert repo.find("u1").name == "Ada"

# good: a fake at the mail boundary; assert the code's decision
mailer = FakeMailer()
signup(users, mailer, "ada@example.com")
assert mailer.sent[0].subject == "Welcome"
```

### 6. Use production-like data

Synthetic stubs miss the validation, formatting, and encoding the real system applies. Use values shaped like production: real-looking names, valid addresses, properly formatted dates and IDs.

```python
# bad
user = create_user("test", "a@b", "123")

# good
user = create_user("Ada Lovelace", "ada@example.com", "GB")
```

### 7. Wait on conditions, never fixed delays

A fixed delay is either too short (flaky) or too long (slow), and it never says what it was waiting for. Wait for the condition, with a timeout that fails the test.

```python
# bad
start_worker()
time.sleep(0.5)
assert worker.finished

# good
start_worker()
wait_until(lambda: worker.finished, timeout=5)
assert worker.processed == 3
```

### 8. Give branches and error paths their own tests

A behaviour that depends on a branch or a failure path is proven only when that path runs. The happy path alone leaves the error behaviour unproven.

```python
# bad: only the accepted input
def test_parse():
    assert parse("1") == 1

# good: the branch and the failure path each have a test
def test_parse_reads_an_integer():
    assert parse("1") == 1

def test_parse_rejects_text():
    with pytest.raises(InvalidInput):
        parse("one")
```

## Tests that prove nothing

Leave these out. They add maintenance without catching a regression, and a suite full of them makes the real checks harder to see.

### A mock asserted against its own return value

The assertion only repeats what the test configured; the code under test never contributes.

```python
# bad
gateway = Mock()
gateway.load.return_value = {"name": "Ada"}
assert gateway.load("u1") == {"name": "Ada"}

# good: assert the code's result after the boundary fails
gateway = FakeGateway(error=Timeout())
profile = load_profile(gateway, "u1")
assert profile.status == "unavailable"
```

### Pure delegation

A method that forwards one call and returns it unchanged has no behaviour of its own; the test covers the mock.

```python
# bad
def test_save_delegates():
    repo = Mock()
    Service(repo).save(item)
    repo.save.assert_called_once_with(item)

# good: prove the transformation the service adds
def test_save_trims_and_returns_the_stored_item():
    repo = InMemoryRepo()
    saved = Service(repo).save(Item(name="  Ada  "))
    assert saved.name == "Ada"
```

### Constructor plumbing

Storing an argument unchanged is the language runtime's job. Test a constructor only when it validates, formats, encodes, or combines values.

```python
# bad
config = Config(retries=3)
assert config.retries == 3

# good: the constructor adds validation
with pytest.raises(ValueError):
    Config(retries=-1)
```

### Constants

A constant asserted against itself fails only if someone renames it, which is a refactor.

```python
# bad
assert MAX_RETRIES == 3

# good: prove the behaviour the constant configures
gateway = AlwaysFails()
load_with_retry(gateway)
assert gateway.calls == MAX_RETRIES
```

### Duplicates of a sibling's test

Two sibling paths that share the same guard and produce the same observable result need one test, not one per sibling. Give a sibling its own test only when its outcome differs.

```python
# bad: same arrangement and assertion twice
def test_delete_missing_user_is_ignored():
    assert delete_user(store, "nobody") is None

def test_delete_missing_group_is_ignored():
    assert delete_group(store, "nobody") is None

# good: one guard test, plus the sibling whose outcome differs
def test_delete_missing_user_is_ignored():
    assert delete_user(store, "nobody") is None

def test_delete_missing_group_reports_a_missing_reference():
    assert delete_group(store, "nobody") == MissingReference("group")
```

## Coverage and timing are not targets

**Coverage is never a target.** No percentage gate, no "add tests until coverage reaches a number". A line can be executed by a test that proves nothing, and a described behaviour can be missing while the percentage looks healthy. The proof is the red-first check for each described behaviour.

```python
# bad: a percentage stands in for proof
if line_coverage < 90:
    fail_the_build()

# good: no percentage gate; each behaviour's named check was seen failing
# before the change that implements it
```

**No timing targets.** Don't assert how fast code runs, and don't set a target duration for a test or the suite. A condition wait's timeout is a failure bound, not a performance target. When the PRD asks for performance, give it a benchmark or a separate check rather than a stopwatch inside a behaviour test.

```python
# bad: a stopwatch in a behaviour test
assert elapsed_ms < 100

# good: assert the outcome; performance has its own benchmark
assert report.rows == 3
```
