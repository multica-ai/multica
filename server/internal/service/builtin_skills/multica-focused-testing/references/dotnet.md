# .NET

Identify the owning project or solution and inspect repository build scripts
before calling `dotnet test`. Prefer a project file over a whole solution for a
focused run. Check the SDK and `global.json` first: .NET 10 can select either
VSTest or Microsoft.Testing.Platform (MTP), and their CLI contracts differ.

For VSTest, after confirming the adapter's filter support:

```text
["dotnet", "test", "path/to/project.csproj", "--filter", "FullyQualifiedName=Namespace.ClassName.MethodName"]
```

Verify the same filter without executing tests:

```text
["dotnet", "test", "path/to/project.csproj", "--list-tests", "--filter", "FullyQualifiedName=Namespace.ClassName.MethodName"]
```

`FullyQualifiedName` is available in the popular VSTest adapters, but the exact
value format and other filter properties vary. Confirm the discovered name from
`--list-tests`.

Do not reuse that argv for MTP. With the .NET 10 MTP driver, project selection
uses `--project`, while test-related arguments come from the registered
framework extensions and should be placed after a literal `--` when forwarding
would otherwise be ambiguous. Inspect `global.json`, the test framework, and
the local `dotnet test --help` / test-application help before constructing the
filter. Do not assume MSTest, NUnit, xUnit, or custom MTP extensions share a
universal selector.

## Official documentation

- Runner selection in `dotnet test`:
  https://learn.microsoft.com/en-us/dotnet/core/tools/dotnet-test
- VSTest `--filter`, `--list-tests`, and filter properties:
  https://learn.microsoft.com/en-us/dotnet/core/tools/dotnet-test-vstest
- .NET 10 MTP project selection and argument forwarding:
  https://learn.microsoft.com/en-us/dotnet/core/tools/dotnet-test-mtp
