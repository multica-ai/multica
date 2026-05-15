Purpose: Verify that the mobile app (Expo React Native) provides login, issue browsing with pull-to-refresh and load-more, issue filtering, project navigation, search, and internationalization support.

Preconditions: The Multica mobile app is installed on a device or emulator. The backend is reachable from the device. A valid test account exists (tester@multica.com with fixed code 888888 or equivalent).

User flow: Launch the mobile app. On the login screen, enter email and verification code to sign in. After login, the Issues tab should display with a list of issues. Pull down to refresh the list. Scroll to the bottom to trigger load-more pagination. Use the filter controls to filter issues by status or priority. Switch to the Projects tab. Switch to the Mine tab. Use the search feature to find a specific issue by keyword. Switch the app language (if i18n toggle is available) to verify internationalization.

Expected results: Email verification code login works on mobile (same flow as web). The Issues list loads and displays correctly on mobile. Pull-to-refresh triggers a data reload with fresh results. Scrolling to the bottom loads additional issues (pagination). Issue filtering narrows the list by the selected criteria. The Projects and Mine tabs are navigable. Search returns relevant results. The app renders correctly in at least Chinese and English (i18n). The app does not crash when scrolling through long content or many issues.

Notes for automation: Mobile testing requires either a device/emulator with Expo or Detox/Appium. The app scheme is `wujieai_multicam` and Android package is `com.wujieai.multica`. Test on both iOS and Android if possible. For i18n, check that button labels and headings change language.
