# Source and mod analysis that drives phase 3

## Key findings from the TaystJK source code

1. `CMakeLists.txt` defines the dedicated server binary as `taystjkded.${Architecture}`.
2. The project `README.md` shows TaystJK should be extracted into Jedi Academy `GameData/` and still requires the original game.
3. `codemp/CMakeLists.txt` builds `japro-assets.pk3` from `assets/japro/`.
4. The same file installs `japro-assets.pk3` into **`JediAcademy/taystjk/`**, not into `JediAcademy/japro/`.
5. Therefore the correct default `fs_game` for TaystJK bundled mod content is **`taystjk`**.
6. Other mods such as `japlus`, `japro`, `mbii`, or other third-party mods must be treated as **manually installed alternative mod folders**.

## Key findings from `fs_game` research

1. Jedi Academy/OpenJK servers use `+set fs_game <modfolder>` to switch the active mod directory.
2. JA+/JA++ guides show that dedicated servers are typically started with `+set fs_game japlus`.
3. Documentation and community guides also show that a mod-specific `server.cfg` is normally placed in the active mod folder, not always in the root directory.
4. For vanilla/base setups there is normally no separate `fs_game` override, or it is left empty.

## Practical impact on the egg/project

- the default should remain `taystjk`
- the egg should still allow users to change mod folder through variables
- `base` should be treated as a special case so startup can run without `+set fs_game base`
- `server.cfg` should be created in the active mod folder (`taystjk`, `japlus`, `japro`, etc.)
- mod files should not be automatically downloaded by this egg, but added manually by the user
