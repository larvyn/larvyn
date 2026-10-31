# Tareas pendientes

Lista de trabajo para Larvyn. Cada tarea es independiente: se puede pasar tal cual a un agente o hacer a mano. Marca `[x]` al terminar.

## Reglas para cualquier tarea

- Compilar: `wails3 build` (genera `bin/larvyn.exe` y los bindings en `frontend/bindings`).
- Verificar antes de dar algo por terminado: `go vet ./...`, `go test ./...`, `gofmt -l .` (vacío) y `npx tsc --noEmit` dentro de `frontend/`.
- Probar la app siempre con un home aislado para no tocar la configuración real: `LARVYN_HOME=<carpeta temporal>`. `LARVYN_VIEW=<vista>` abre directo en una vista.
- Los proyectos Laravel reales (crm, villas…) solo se usan en modo lectura.
- Bindings: son interfaces con listas que pueden venir `null`; pasar objetos planos y proteger con `?? []`.
- zustand: un selector nunca debe devolver un `[]` o `{}` nuevo en cada llamada (provoca un loop infinito de React).
- Commits: conventional commits en minúsculas, una línea, sin líneas de co-autor.

---

## 1. AGENTS.md con las reglas del proyecto

- [ ] Crear `AGENTS.md` en la raíz con: arquitectura (servicios Go en `package main`, `bridge.php` embebido, vistas React en `frontend/src/views`), comandos de build y test, y las reglas de arriba.
- [ ] Mantenerlo corto: un mapa con reglas y ejemplos, no documentación completa.

## 2. Repositorio remoto

- [ ] Crear el repo en GitHub (privado) y hacer `git remote add origin …` + `git push -u origin master`.

## 3. CI

- [ ] Workflow `.github/workflows/ci.yml` en `windows-latest`: setup Go y Node, `npm ci` en `frontend/`, `go vet`, `go test`, `npx tsc --noEmit`, `wails3 build`.
- [ ] Subir `bin/larvyn.exe` como artifact del workflow.

## 4. Instalador de Windows

- [ ] Generar el instalador NSIS con la tarea ya existente: `wails3 task windows:create:nsis:installer` (ver `build/windows/Taskfile.yml`, que usa `build/windows/nsis/project.nsi`).
- [ ] Revisar que el instalador crea el acceso en el menú Inicio, usa el icono de Larvyn y se desinstala limpio.
- [ ] Documentar el comando en el README.

## 5. Firma de código

- [ ] Conseguir un certificado de firma (Authenticode).
- [ ] Usar las tareas `windows:sign` y `windows:sign:installer` de `build/windows/Taskfile.yml`.
- [ ] Verificar que SmartScreen ya no muestra "aplicación desconocida".

## 6. Auto-update

- [ ] Publicar releases en GitHub con el instalador.
- [ ] Al arrancar, consultar la última release (API de GitHub), comparar con la versión de `build/config.yml` y mostrar un aviso "Nueva versión disponible" con botón para descargar e instalar.
- [ ] Opción en Settings para desactivar la comprobación.

## 7. macOS y Linux

- [ ] Compilar en cada sistema (`wails3 build`) y probar: terminales (PTY), lanzar agentes, stack de servicios, abrir en editor.
- [ ] Revisar `proc_other.go` (matar árbol de procesos, shell por defecto) y la detección de PHP/Composer fuera de Herd.

## 8. Interfaz en español

- [ ] Añadir i18n ligero: un archivo de textos por idioma (`frontend/src/i18n/es.ts`, `en.ts`) y un hook `t()`.
- [ ] Pasar todos los textos de las vistas, la palette y los toasts por `t()`.
- [ ] Selector de idioma en Settings (por defecto el del sistema), guardado en los settings.

## 9. Más integración con Herd

- [ ] Leer los sitios de Herd (`herd sites` / `herd links`) y mostrarlos en Overview.
- [ ] Cambiar la versión de PHP por sitio (`herd isolate <version>`) desde la UI.
- [ ] Activar/desactivar HTTPS por sitio (`herd secure` / `herd unsecure`).

## 10. Tema claro

- [ ] Definir los tokens de color del tema claro en `frontend/src/styles/app.css` junto a los del oscuro.
- [ ] Ajustar el tema de xterm para que las terminales sigan siendo legibles.
- [ ] Selector en Settings: oscuro, claro o según el sistema.
