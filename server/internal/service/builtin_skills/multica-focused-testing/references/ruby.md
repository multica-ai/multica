# Ruby

Inspect `Gemfile`, repository scripts, and runner configuration. Use
`bundle exec` when the repository is Bundler-managed so the selected runner
version and plugins match the project.

For RSpec, a file target is:

```text
["bundle", "exec", "rspec", "spec/path/to/example_spec.rb"]
```

RSpec can select an example or group by a line in the file:

```text
["bundle", "exec", "rspec", "spec/path/to/example_spec.rb:<line>"]
```

Do not apply RSpec syntax to Minitest, Rails test tasks, or custom runners.
For example, Rails' Minitest runner has its own file-and-line form:

```text
["bin/rails", "test", "test/models/user_test.rb:<line>"]
```

Follow the repository's runner-specific command and expected scope.

## Official documentation

- Bundler execution context and its RSpec example:
  https://bundler.io/man/bundle-exec.1.html
- RSpec file-and-line selection:
  https://rspec.info/features/3-12/rspec-core/command-line/line-number-appended-to-path/
- Rails Minitest file, name, and line selection:
  https://guides.rubyonrails.org/testing.html#the-rails-test-runner
