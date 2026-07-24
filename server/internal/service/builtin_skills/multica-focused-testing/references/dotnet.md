# .NET

Identify the owning project or solution and inspect repository build scripts
before calling `dotnet test`. Prefer a project file over a whole solution for a
focused run.

After confirming the adapter's filter support:

```text
["dotnet", "test", "path/to/project.csproj", "--filter", "FullyQualifiedName=Namespace.ClassName.MethodName"]
```

Filter properties and operators vary by test adapter. Confirm them from the
installed adapter and repository configuration; do not assume NUnit, xUnit,
MSTest, or custom adapters share identical names.
