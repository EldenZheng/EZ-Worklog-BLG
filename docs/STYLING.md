# UI styling guide

Use the existing controls and layouts before adding a new visual treatment.
The reference for floating capsules and scrolling content is the Kanban column.

## Rounded panels and overflow

- Use `shell.Panel` for an inset grey panel and `shell.PanelFrame` when its
  content supplies the padding. Both use the shared `CellCornerRadius`.
- A fixed bubble beside scrolling content floats **over** that content. Paint
  the scroll first and the bubble last. Let the scroll extend behind part of
  the bubble rather than ending at a hard, full-width blank strip.
- Reserve space **inside the scroll content** for the floating bubble and a
  small gap. At the end of the scroll, the final action must be reachable above
  a footer or below a header. Overlap must never hide the final control.
- Keep the whole scroll inside its column or panel viewport. Content may pass
  behind a capsule, but must not escape into the page's navigation or identity
  controls.
- The capsule's surface must absorb clicks so hidden controls cannot activate.
  Keep its own child buttons interactive, with a normal cursor over neutral
  areas of the capsule.
- Suppress stock scroll shadows with `shell.NoScrollShadow`. Keep the scroll
  bar along the outer edge, as in Kanban.
- Measure a wrapping capsule at its assigned width before using its height.

References:

- [Kanban column construction](../internal/menus/kanban/kanban.go): scroll first,
  floating heading second.
- [Floating header and content inset](../internal/shared/shell/floating_header.go):
  `FloatingHeaderLayout` and `ColumnInset.Top` / `Bottom`.
- [Settings footer](../internal/menus/settings/sidebar.go): the matching layout
  for the Updates bubble at the bottom of the storage scroll.

## Section headings and disclosure arrows

- Use gold, bold section headings at `theme.TextSize()+4`.
- Put a section's underline on its own line **below** the heading. Use
  `shell.SectionTitle` for ordinary headings. Its separator inherits the
  app theme's muted gold, as does `widget.NewSeparator()` between content groups.
- A collapsible section with no other trailing controls uses the whole heading
  width, with its disclosure arrow at the **far right**. Use
  `shell.CollapseLine(label, nil, body, ...)`.
- Keep totals and explanatory text below the heading. Let prose wrap; do not
  stretch the sidebar to fit a path or long status message.
- Give Settings' Storage, Data + Backups and History Backup sections
  `theme.Padding()*4` of top padding. All three fold, light gold under the
  pointer and show their total in gold. All three start collapsed and keep
  the user's open/closed choice while refreshing that Settings view.
- The Updates bubble has a gold title and no extra divider around it.
- Avoid redundant captions such as "Preferred backup folder" above a location
  that is already shown in the History backup section.

## Buttons and colours

Choose buttons by their action. All roles live in
[`shell/buttons.go`](../internal/shared/shell/buttons.go).

| Action | Role |
| --- | --- |
| Write to GitHub | `GitHubButton`; `CompactGitHubButton` on cards |
| Accept a popup choice or start an update | `ConfirmButton` |
| Delete, clear or discard | `DestructiveButton`; `RemovalButtons` for a pair |
| Local save, cancel, close, fetch or refresh | Plain gold button |
| Icon-only action or Back | `QuietButton` |

Use `DialogButtons` with the way out first and action last. Do not assign
importance colours inside a menu or use stock Fyne confirmation dialogs.
Keep storage refresh's glyph gold even though its button has no fill.

Organisation colours identify organisations. Storage categories have their own
stable, distinct palette: do not cycle the organisation palette across a longer
list of categories. Use the same colour for a category's dot and meter segment.

Field focus uses the app's gold in every menu and popup. `GoldTheme` supplies
gold for the primary accent (Fyne uses it for input borders and carets), with a
translucent gold focus wash and text selection. Keep validation errors red and
use the button roles for green GitHub actions and destructive red actions.

All option dropdowns use `shell.NewGoldSelect`: a muted gold field with a
rounded outline and gold chevron, opening a dark inset menu with a gold edge.
The selected row is gold with dark words and a bare check at the right. Keep
one travelling gold hover across the rows. Selection uses the top menu's
click-origin gold spread and ink transition, draining the previous row and
drawing the new check. By default the action runs immediately and the popup
starts folding at once, with no confirmation pause. Preserve search, keyboard navigation, status
colour dots, disabled states and reduced motion through the shared control.
Set `StayOpen` for Status and Report's **Worklog** selectors: selecting an
option animates the row and check while leaving the menu open. Outside click,
Escape and clicking the field again dismiss it with the usual closing motion.

## Confirmations and startup notices

- Use `ui.ShowConfirmation` for a decision and `ui.ConfirmRemoval` for a
  plain-text removal prompt. The gold heading and full-gold underline sit
  above an inset message panel. Buttons keep their role's height and radius.
- Use `ui.ShowLongNotice` for multi-section notices such as “What's new”.
  It uses the same frame with a taller reading area. The body scrolls while
  the checkbox and acknowledgment button stay in the footer.
- Use `ui.ShowEditorPopup` for full forms such as the Log Work editors.
  Place Back and Save actions beside the title above the gold underline.
  The inset form panel below it controls its own scrolling. Use
  `shell.FadeQuietButton` for Back's 600 ms hover fade.
- Wrap popup preference checkboxes with `shell.GoldTickChoice` to give the
  box and its label the Kanban filter's gold hover and animated tick.
- Push progress and push results use their dedicated status popups.

## Motion and feedback

- Reuse `shell.Fold` through the collapse helpers. Relayout with `shell.Reflow`
  on each frame so nearby content follows the section, as in Log Work.
- Geometry-only animations keep cached text and shapes through their final
  frame. Use `shell.Repaint` after layout; a full subtree refresh at completion
  can cause a hitch just as the motion settles.
- Keep collapse state when refreshing a section's data.
- Use the shared hover, press, tick and popup motion. Do not substitute a snap
  transition or a separate timing for the same kind of control.
- Show and close popups with `shell.ShowDialog` / `CloseDialog`, or the matching
  stock-dialog and popup wrappers.
- Only show hover feedback on controls that have an action.

## Checking a change

Use focused geometry and motion tests for overlap, final-control reachability,
arrow alignment and intermediate fold frames. Off-screen demo renders are
allowed. Build with `build.ps1`; the user runs the app for the visual check.
Do not launch the executable, drive the mouse or capture the running app.
