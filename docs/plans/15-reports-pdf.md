# 15 · Exportar el reporte a PDF (fase tardía)

## Objetivo

Descargar el reporte de un periodo como PDF, con **solo el resumen** (totales y rankings), sin la lista de movimientos (guía §11).

> Fase tardía: se implementa después de los planes 00 a 12. Hoy Reportes muestra "PDF · próximamente", inactivo.

## Pantallas de Android que cubre

- **Reportes → PDF** (`ORBITA_SPEC` §7.8).

## Módulo

`reports`.

## Endpoints

| Método y ruta | Query | Respuesta |
|---|---|---|
| `GET /reports/summary.pdf` | `from`, `to` | `200 application/pdf`, `Content-Disposition: attachment; filename="orbita-2026-09.pdf"` |

## Reglas

- Las mismas cifras que `GET /reports/summary` (plan 07): se genera desde el mismo caso de uso, no desde otra consulta.
- Contenido en español: encabezado con el periodo, un bloque por moneda (ingresos, gastos, balance) y los dos rankings con sus barras y porcentajes. Formatos de `ORBITA_SPEC` §4 (`S/ 1,245.80`, `2 oct 2026`, `37.2%`). Fuente Manrope, como la app.
- Se genera en el proceso y se transmite a la respuesta; no se guarda copia en el servidor (ni en R2).
- El nombre del archivo sale solo de fechas validadas, nunca de texto del usuario.
- Límite: 10 por hora por usuario.

## Preguntas antes de empezar

1. Librería de PDF en Go (por ejemplo `go-pdf/fpdf` o `johnfercher/maroto`): se elige con una prueba de las dos.
2. ¿Se comparte desde la app (hoja de compartir de Android) o solo se guarda en Descargas?

## Variables nuevas para el servidor

Ninguna previstas.
