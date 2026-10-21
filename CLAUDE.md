# atm-orbita-api

API del proyecto Orbita, escrita en Go con Gin. Repo: `git@github.com:AtmosferaSoltec/atm-orbita-api.git`

Este archivo contiene las reglas del proyecto. Se irán agregando nuevas reglas a medida que se definan.

## Regla 1: Ramas y flujo de promoción

Hay exactamente 3 ramas:

| Rama | Propósito |
|---|---|
| `develop` | Aquí se trabaja todo: cambios nuevos, pruebas y testeos. |
| `main` | Guarda el proyecto con los cambios de `develop` que ya están funcionales y reales. |
| `production` | Envía los cambios al servidor VPS Linux mediante un workflow de GitHub. |

El flujo es siempre `develop` → `main` → `production`.

- **Prohibido** pasar `develop` directamente a `production`. Todo cambio debe pasar primero por `main`.
- El trabajo nuevo nunca se hace directamente sobre `main` ni `production`.

### Workflow de production

El workflow de GitHub Actions que corre al actualizar `production` tiene 3 jobs, en este orden:

1. **check**: revisa el código y verifica que el proyecto compile.
2. **build**: construye el proyecto (la imagen) en GitHub, no en el VPS, para no consumir RAM del servidor.
3. **deploy**: GitHub se conecta al VPS Linux y este obtiene la imagen nueva.

## Para quien implemente el proyecto

Lee [AGENT_GUIDE.md](AGENT_GUIDE.md) completo antes de empezar: describe la arquitectura, el despliegue, el orden de trabajo y las decisiones ya tomadas.
