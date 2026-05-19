Purpose: Verify that a project detail page exposes the new Gantt view and renders scheduled issues on a timeline.

Preconditions: The Multica web app is reachable. The user is signed in. A project exists with at least two issues that have scheduled start/end or due-date data. The project detail page is accessible from the Projects list.

User flow: Navigate to Projects and open the project detail page. Locate the project view switcher and choose the Gantt view. Verify scheduled issues appear as timeline rows or bars. Create or edit an issue in the same project so it has a scheduled date range, then return to the Gantt view. Verify the changed issue appears or updates in the timeline without breaking the existing board/count display.

Expected results: The project detail page offers a Gantt view alongside the existing project views. Only issues with schedule data appear in the Gantt timeline. Each visible timeline item shows enough issue context, such as title or identifier, to open the issue. Updating schedule data refreshes the Gantt data source, and the existing board counts remain available on the project page.

Notes for automation: Locate the view switcher by visible labels such as "Gantt", "Board", or "List". Use visible issue titles/identifiers to confirm timeline rows. If seeded issues are missing, create two project issues with schedule fields before opening the Gantt view.
