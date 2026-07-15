# FIR-3172 plan coverage

Every implementation package has a named automated proof. `TestMiniAppPlanCoverageReferencesExist` fails when a package or referenced test disappears.

No gate is complete merely because a unit test is green. The proof must cross
the production boundary named below; the final release gate also requires CI,
the Docker isolation test, a fully migrated PostgreSQL database, and the
Playwright Allergen Formatter flow.

| Gate | Product claim | Automated proof | Production boundary |
|---|---|---|---|
| G1 | Production workflow execution contains no fake adapter path. | `TestProductionMiniAppsContainNoWorkflowFakes` | Compiled production package and real adapters |
| G2 | Every Registry action is attributable to member, app, version, and run. | `TestBrokerIssuesAppBoundPersonalKeyAndCachesIt` plus the Registry audit integration in FIR-3172 | Registry session exchange and audit rows |
| G3 | Schedule, webhook, data event, manual, and chat start durable Hatchet runs. | `TestAllFiveWorkflowTriggersHaveProductionRoutes` | PostgreSQL run rows and Hatchet dispatch |
| G4 | Every documented SDK method has a live route, including API and MCP `connections.call`. | `TestAppConnectionCallAppliesConnectionAndHumanCeilings` and `TestAppConnectionCallSupportsApprovedMCPToolAndHumanCeiling` | Approved Connections with server-side credentials |
| G5 | The app grant and acting person's access remain independent hard ceilings. | `TestAppConnectionCallAppliesConnectionAndHumanCeilings` plus Registry permission integration | Registry and Connection policy enforcement |
| G6 | One app failure cannot affect another app. | `container-integration.test.mjs` | Docker daemon and two live app containers |
| G7 | App failures expose no stack traces, paths, or credentials. | `one failed worker does not affect runtime health` | Spawned worker and user-facing response |
| G8 | A real issue or chat card waits once and resumes without duplicate writes. | `TestViewSubmissionCanResumeOneRequest` and `cerebro-mini-apps.spec.ts` | PostgreSQL comment/request/submission rows and browser iframe |
| G9 | Canonical documentation covers the shipped SDK and workflow contract. | `mini-app documentation contract` | `docs/mini-apps/` |
| G10 | Every work package stays mapped to a proof that exists. | `TestMiniAppPlanCoverageReferencesExist` and `mini-app documentation contract` | Coverage document and referenced test files |
| G11 | Each app has its own container, process, and scoped mount. | `container-integration.test.mjs` | Docker daemon, container process, and bundle mount |

| Package | Delivered surface | Automated proof |
|---:|---|---|
| 0 | Per-app container, mount, limits, and egress | `apps/cerebro-apps-runtime/container-integration.test.mjs#killing app A leaves app B healthy on an internal-only network` |
| 1 | Real Registry adapters and run trace | `server/internal/cerebro/apps/workflow_adapters_test.go#TestRegistryAdapterCallsRealV1RoutesWithRunTrace` |
| 2 | Hatchet dispatch and private worker contract | `server/internal/cerebro/apps/workflow_dispatch_test.go#TestSubmitWorkflowRunUsesThePrivateWorkerContract` |
| 3 | Five real trigger routes | `server/internal/cerebro/apps/workflow_triggers_test.go#TestAllFiveWorkflowTriggersHaveProductionRoutes` |
| 4 | Interactive card pause and resume | `server/internal/cerebro/apps/handler_test.go#TestViewSubmissionCanResumeOneRequest` |
| 5 | SDK route integrity and scoped Connections | `server/internal/cerebro/runtime/api_connection_resolver_test.go#TestAppConnectionCallAppliesConnectionAndHumanCeilings` |
| 6 | User-safe error masking | `apps/cerebro-apps-runtime/runtime.test.mjs#one failed worker does not affect runtime health` |
| 7 | Complete docs and Multica skill source | `server/internal/cerebro/apps/plan_coverage_test.go#TestMiniAppDocumentationAndSDKSurface` |
| 8 | Lifecycle permissions, owner, deletion, admin overview | `server/internal/cerebro/apps/admin_test.go#TestMiniAppLifecyclePermissionsAndCascadeDeletion` |
| 9 | FIR-154 Allergen Formatter | `apps/cerebro-apps-runtime/runtime.test.mjs#allergen fixture makes one AI call on the personal key` |
| 10 | Real nested catalog folders | `server/internal/cerebro/apps/folders_test.go#TestAppFoldersSupportNestedRenameAndMove` |
| 11 | Plan-to-code gate | `server/internal/cerebro/apps/plan_coverage_test.go#TestMiniAppPlanCoverageReferencesExist` |
