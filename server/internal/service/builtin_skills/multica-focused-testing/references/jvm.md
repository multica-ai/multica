# JVM

Use the repository wrapper (`gradlew` or `mvnw`) and identify the owning module
before selecting a test. Build configuration may add plugins, profiles, or
environment required by the suite.

For Gradle, after confirming the task path:

```text
["./gradlew", ":module:test", "--tests", "package.ClassName.methodName"]
```

Use `gradlew.bat` on Windows. If the repository's wrapper exposes
`--test-dry-run`, add it first to inspect the selected tests without executing
them. Filters configured in the build script still apply alongside `--tests`.

For Maven Surefire, after confirming the module and plugin:

```text
["./mvnw", "-pl", "module", "-Dtest=ClassName#methodName", "test"]
```

Use `mvnw.cmd` on Windows. The current Surefire documentation scopes its
`ClassName#methodName` example to JUnit 4.x and TestNG. For JUnit 5 or another
provider, use a method selector only after the repository's installed plugin
and provider confirm support.

Do not assume these selectors apply to custom Gradle tasks, Maven Failsafe,
integration-test source sets, parameterized cases, or other JVM runners.
Inspect the build files and task/plugin help first.

## Official documentation

- Gradle wrapper, project task paths, and task options:
  https://docs.gradle.org/current/userguide/command_line_interface.html
- Gradle `--tests` filtering and `--test-dry-run`:
  https://docs.gradle.org/current/userguide/java_testing.html#test_filtering
- Maven Surefire single-class and method selectors:
  https://maven.apache.org/surefire/maven-surefire-plugin/examples/single-test.html
