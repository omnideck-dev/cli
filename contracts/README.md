# Machine-readable contracts

These schemas describe the public process boundary consumed by Desktop and
other automation. The JSON protocol version and command payload schema versions
are independent: for example, JSON contract 3 currently carries runtime status
schema 4.

Consumers must ignore unknown fields. An incompatible removal, rename, type
change, enum change, or semantic change requires a new applicable contract or
payload schema version. Additive optional fields do not.

The black-box tests in `tests/releasecontract` exercise the same contract
against compiled and packaged executables.
