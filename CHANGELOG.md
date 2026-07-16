# Changelog

## [0.3.0](https://github.com/deploymenttheory/go-bindings-winrt/compare/v0.2.0...v0.3.0) (2026-07-16)


### Features

* composition instantiate ([7b91f31](https://github.com/deploymenttheory/go-bindings-winrt/commit/7b91f3132ed38c4bbe473ed8e8f2447090090882))
* composition, instantiate-only — 704 composable classes un-skip ([f9b22d9](https://github.com/deploymenttheory/go-bindings-winrt/commit/f9b22d9868f8a2c07ed36ee10a13747345aec272))
* element-generic + writable Go-implemented collections ([f85d352](https://github.com/deploymenttheory/go-bindings-winrt/commit/f85d3525d2e8f76f6830ec756fc33e9eab01ff84))
* generated Await() on WithProgress async operations ([9145e05](https://github.com/deploymenttheory/go-bindings-winrt/commit/9145e059d83abd103c16349f11f4a0add9201aa5))

## [0.2.0](https://github.com/deploymenttheory/go-bindings-winrt/compare/v0.1.0...v0.2.0) (2026-07-15)


### Features

* async ([5a95388](https://github.com/deploymenttheory/go-bindings-winrt/commit/5a953880f8c1da5466b1b7ba32665de712a9755f))
* async awaiting — delegate params on methods + generated Await() ([05f2d32](https://github.com/deploymenttheory/go-bindings-winrt/commit/05f2d32fee1d7f8b66548ab696679a75bf1be89d))
* Bluetooth + Management namespaces; heap-escaped out-params kill the stack-move flake ([3ec54f4](https://github.com/deploymenttheory/go-bindings-winrt/commit/3ec54f434dae76a1fed97f0cd62bfd0bcf089738))
* Bluetooth + Management namespaces; heap-escaped out-params kill… ([12f4957](https://github.com/deploymenttheory/go-bindings-winrt/commit/12f49571148dbc6d0cb8cf468456456620296ad1))
* emit events with monomorphized typed Go handler constructors ([2dc23f3](https://github.com/deploymenttheory/go-bindings-winrt/commit/2dc23f399c3a090cbc7b333addf72471d4839b61))
* emit events with monomorphized typed Go handler constructors ([bb4b7cc](https://github.com/deploymenttheory/go-bindings-winrt/commit/bb4b7cc9dbbcbe0783298fa9040517e8ceb2a522))
* emit generic interface instantiations as monomorphized types ([e6f35a2](https://github.com/deploymenttheory/go-bindings-winrt/commit/e6f35a27f37454cfc50ee2deed29acb0f2c76e34))
* emit generic interface instantiations as monomorphized types ([15f66aa](https://github.com/deploymenttheory/go-bindings-winrt/commit/15f66aaa0b3f54eb4a2823ada751f29d605714b3))
* emit statics accessors and factory constructors ([6d21840](https://github.com/deploymenttheory/go-bindings-winrt/commit/6d218405bed4eee4055edf9265c52ee97f94b677))
* emit statics accessors and factory constructors ([b764ab4](https://github.com/deploymenttheory/go-bindings-winrt/commit/b764ab4d5da3442c792f5b1d48c36ef0496b5d61))
* emit the full WinRT surface — all 282 namespaces ([74d5e20](https://github.com/deploymenttheory/go-bindings-winrt/commit/74d5e20a8e1e122fa4372341914a2fb0b347cd7a))
* emit the full WinRT surface — all 282 namespaces ([d0700c6](https://github.com/deploymenttheory/go-bindings-winrt/commit/d0700c6463af5bb95c985a86b3d5384824b56c48))
* Go-implemented WinRT collections + stack-growth-safe callback dispatch ([e9b5cb4](https://github.com/deploymenttheory/go-bindings-winrt/commit/e9b5cb4c9c07a4a89536974f44222011d67a2a57))
* land the wave-1 stack — Calendar vertical, generator (ingest + emit), delegate runtime ([e4778c1](https://github.com/deploymenttheory/go-bindings-winrt/commit/e4778c15e840562b163bb0d5052531b6cd752620))
* pinterface IID engine — derived IIDs for parameterized-type instantiations ([7f4b918](https://github.com/deploymenttheory/go-bindings-winrt/commit/7f4b91895f39364ea57d58bcd0ace8cd7a81c850))
* pinterface IID engine — derived IIDs for parameterized-type instantiations ([707da32](https://github.com/deploymenttheory/go-bindings-winrt/commit/707da324e731b034258010e69ef93e2b035b6c3a))
* Windows.UI.Notifications vertical — the toast surface, generated ([cfe5a26](https://github.com/deploymenttheory/go-bindings-winrt/commit/cfe5a26ca08c3cb9e117f3a706e35601da7aca89))
* Windows.UI.Notifications vertical — the toast surface, generated ([aff2509](https://github.com/deploymenttheory/go-bindings-winrt/commit/aff2509040955e84e3f9a750d210899cf394451d))

## 0.1.0 (2026-07-15)


### Features

* bootstrap the module with the hand-written WinRT runtime layer ([89d4041](https://github.com/deploymenttheory/go-bindings-winrt/commit/89d4041bb1157d19a2ebf0b8a693aaa9da0e2b73))
* bootstrap the module with the hand-written WinRT runtime layer ([3916d38](https://github.com/deploymenttheory/go-bindings-winrt/commit/3916d385bce1a8b28075c42a102b3a44f0a0a7e0))


### Bug Fixes

* **ci:** let include:scope derive the dependabot commit scope ([0600765](https://github.com/deploymenttheory/go-bindings-winrt/commit/0600765e36b8ae440280b4b65fddf53a973ec076))
* **ci:** stop dependabot doubling the commit scope to chore(deps)(deps) ([dcd8446](https://github.com/deploymenttheory/go-bindings-winrt/commit/dcd84464bd5b1be8f19a3110d88b1c4eea4b9d91))
* **docs:** add a complete Related projects section to the README ([3c2db08](https://github.com/deploymenttheory/go-bindings-winrt/commit/3c2db08bafa0b5a27b3cedac135dab034454b4c3))

## Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Releases and their notes are managed automatically by release-please.
