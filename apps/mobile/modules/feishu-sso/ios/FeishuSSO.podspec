Pod::Spec.new do |s|
  s.name           = 'FeishuSSO'
  s.version        = '0.0.1'
  s.summary        = 'Expo wrapper over LarkSSOSDK for in-house Feishu OAuth.'
  s.author         = 'Lilith'
  s.homepage       = 'https://multica.lilithgames.com'
  s.platforms      = { :ios => '15.1' }
  s.source         = { :git => '' }
  s.static_framework = true
  s.swift_version  = '5.0'

  s.source_files = '*.{swift}'

  s.dependency 'ExpoModulesCore'
  # LarkSSOSDK MUST be declared as a dependency here, not only in the app
  # Podfile. `import LarkSSOSDK` in FeishuSSOModule.swift is resolved while
  # compiling THIS pod, and a Swift module is only visible to a pod that
  # declares the dependency — without it the build fails with
  # "Unable to find module dependency: 'LarkSSOSDK'".
  #
  # The custom spec repo (github.com/volcengine/volcengine-specs.git) that
  # hosts LarkSSOSDK is declared via `source` in the app-level Podfile by
  # the withFeishuSSO config plugin — a podspec dependency can't carry its
  # own source URL, so the consumer Podfile's `source` lines are what let
  # CocoaPods resolve this version. Both pieces are required.
  s.dependency 'LarkSSOSDK', '1.2.0'
end
