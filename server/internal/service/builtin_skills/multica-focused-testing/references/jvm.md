# JVM

Use the repository wrapper (`gradlew` or `mvnw`) and identify the owning module
before selecting a test. Build configuration may add plugins, profiles, or
environment required by the suite.

For Gradle, after confirming the task path:

```text
["./gradlew", ":module:test", "--tests", "package.ClassName.methodName"]
```

For Maven Surefire, after confirming the module and plugin:

```text
["./mvnw", "-pl", "module", "-Dtest=ClassName#methodName", "test"]
```

Do not assume these selectors apply to custom Gradle tasks, Failsafe,
integration-test source sets, or other JVM runners. Inspect the build files and
task/plugin help first.
