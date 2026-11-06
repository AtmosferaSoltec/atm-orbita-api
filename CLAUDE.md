# atm-orbita-api

API del proyecto Orbita, escrita en Go con Gin. Repo: `git@github.com:AtmosferaSoltec/atm-orbita-api.git`

Este archivo contiene las reglas del proyecto. Se irán agregando nuevas reglas a medida que se definan.

## Regla 1: Ramas y flujo de promoción

Hay exactamente 2 ramas (desde el 4 nov 2026; la rama `develop` se eliminó):

| Rama | Propósito |
|---|---|
| `main` | Aquí se trabaja todo: cambios nuevos, pruebas y testeos. Es la rama por defecto. |
| `production` | Envía los cambios al servidor VPS Linux mediante un workflow de GitHub. |

El flujo es siempre `main` → `production`.

- **Prohibido** subir cambios directo a `production`: todo cambio entra primero a `main`.
- Subir a `main` no despliega nada. Subir a `production` sí: promover `main` a `production` lo decide el usuario.
- No se vuelve a crear `develop`.

### Workflow de production

El workflow de GitHub Actions que corre al actualizar `production` tiene 3 jobs, en este orden:

1. **check**: revisa el código y verifica que el proyecto compile.
2. **build**: construye el proyecto (la imagen) en GitHub, no en el VPS, para no consumir RAM del servidor.
3. **deploy**: GitHub se conecta al VPS Linux y este obtiene la imagen nueva.

## Para quien implemente el proyecto

Lee [AGENT_GUIDE.md](AGENT_GUIDE.md) completo antes de empezar: describe la arquitectura, el despliegue, el orden de trabajo y las decisiones ya tomadas.
