Migraciones de goose (SQL), embebidas en el binario. Reglas en AGENT_GUIDE.md, seccion 7.1:

- Un archivo por cambio, numerado y con el modulo en el nombre: 00003_ledger_create_categories.sql
- Cada archivo con sus secciones "-- +goose Up" y "-- +goose Down".
- Una migracion aplicada nunca se edita: se corrige con otra nueva.
- Seguras con la version anterior aun corriendo: primero agregar, despues quitar.

Este archivo existe para que la carpeta tenga algo que embeber mientras no hay
migraciones. goose solo lee los .sql.
