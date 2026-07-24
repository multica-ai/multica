# Ruby

Inspect `Gemfile`, repository scripts, and runner configuration. Use
`bundle exec` when the repository is Bundler-managed so the selected runner
version and plugins match the project.

For RSpec, a file target is:

```text
["bundle", "exec", "rspec", "spec/path/to/example_spec.rb"]
```

RSpec can select an example by line or documented id. Confirm the installed
version's selector before using it.

Do not apply RSpec syntax to Minitest, Rails test tasks, or custom runners.
Follow the repository's runner-specific command and expected scope.
