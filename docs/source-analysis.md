# Källkods- och modanalys som styr fas 3

## Huvudfynd från TaystJK-källkoden

1. `CMakeLists.txt` definierar dedikerad server som `taystjkded.${Architecture}`.
2. `README.md` visar att TaystJK ska extraheras till Jedi Academy `GameData/` och fortfarande kräver originalspelet.
3. `codemp/CMakeLists.txt` skapar `japro-assets.pk3` från `assets/japro/`.
4. Samma fil installerar `japro-assets.pk3` till **`JediAcademy/taystjk/`**, inte till `JediAcademy/japro/`.
5. Därför är korrekt default-`fs_game` för TaystJK:s inbyggda modinnehåll **`taystjk`**.
6. Andra mods som `japlus`, `japro`, `mbii` eller andra tredjepartsmods måste ses som **manuellt installerade alternativa modmappar**.

## Huvudfynd från research om `fs_game`

1. Jedi Academy/OpenJK-servrar använder `+set fs_game <modmapp>` för att byta aktiv modkatalog.
2. JA+/JA++-guider visar att man typiskt startar dedikerad server med `+set fs_game japlus`.
3. Dokumentation och community-guider visar också att modspecifik `server.cfg` normalt ligger i den aktiva modmappen, inte alltid i huvudkatalogen.
4. För base/vanilla används normalt ingen separat `fs_game`-override alls, eller ett tomt värde.

## Praktisk påverkan på egg/projektet

- default ska fortsätta vara `taystjk`
- egg ska samtidigt låta användaren byta modmapp via variabler
- `base` ska hanteras särskilt så att startup kan köras utan `+set fs_game base`
- `server.cfg` bör skapas i aktuell modmapp (`taystjk`, `japlus`, `japro`, osv.)
- själva modfilerna ska inte automatiskt hämtas av detta egg, utan läggas in manuellt av användaren
