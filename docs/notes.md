# Notes

## Phase 4 conclusions

1. TaystJK dedicated server is built as `taystjkded.<arch>`.
2. TaystJK bundled jaPRO-oriented content is installed into `taystjk/`, not a separate `japro/` folder.
3. Therefore the egg default should be `FS_GAME_MOD=taystjk`.
4. External mods still need to be installed manually as separate folders.
5. Egg startup should not use both `FS_GAME_MOD` and a separate `FS_GAME_STARTUP`, because they can diverge and create hard-to-debug errors.
6. The Docker image should be built from the project root with `docker build -f docker/Dockerfile ... .` so that `COPY scripts/entrypoint.sh` works reliably.
