# Contributing

Use Apache-2.0 SPDX headers in original source files. Keep hardware profiles
specific to tested SKU/revision pairs. Include the board/image evidence when
changing boot selection, slot grouping, signing or recovery behavior.

Before a pull request run `make check`, `make demo`, and `make dist`. Unit and
protocol tests must not write real block devices or request a host reboot.
Use the simulator or the private D-Bus service in tests. Never add a production
"skip signature", "force compatible", or automatic journal-reset option.

State schema changes must include migration and rollback tests. The current
journal schema is 1 and unsupported schema versions fail startup. Keep Fleet
events backward-compatible or introduce a new API version.

Follow conventional commit messages where helpful. A PR should explain the
failure or behavior it addresses, changed behavior, tests run, and remaining
device-specific qualification. Do not describe mock tests as hardware tests.
