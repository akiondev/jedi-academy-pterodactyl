# Notes

## Fas 4 slutsatser

1. TaystJK:s dedikerade server byggs som `taystjkded.<arch>`.
2. TaystJK:s förinstallerade jaPRO-orienterade innehåll installeras i `taystjk/`, inte i en separat `japro/`-mapp.
3. Därför ska default i egget vara `FS_GAME_MOD=taystjk`.
4. Externa mods måste fortfarande installeras manuellt som egna mappar.
5. Egg-startupen bör inte använda både `FS_GAME_MOD` och en separat `FS_GAME_STARTUP`, eftersom de kan divergera och skapa felsökta fel.
6. Docker imagen ska byggas från projektroten med `docker build -f docker/Dockerfile ... .` för att `COPY scripts/entrypoint.sh` ska fungera stabilt.
