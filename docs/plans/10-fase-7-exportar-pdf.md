# 10 · Fase 7 — Exportar reportes a PDF

**Objetivo:** que el usuario descargue en PDF el reporte que está viendo en pantalla (mes, año o rango). Hoy la pantalla de Reportes muestra la etiqueta inactiva "PDF · próximamente".

**Tamaño:** mediano. **Depende de:** Fase 4.

## 1. Endpoint

`GET /reports/summary.pdf?from=2026-09-01&to=2026-09-30`

| Parámetro | Uso |
|---|---|
| `from`, `to` | Mismo significado y validaciones que `GET /reports/summary` |

**Decisión del usuario (7 oct 2026): el PDF es solo el resumen.** Totales y rankings por categoría, sin lista de movimientos. No hay parámetro para pedir el detalle.

Respuesta: `200` con `Content-Type: application/pdf`, `Content-Disposition: attachment; filename="orbita-reporte-2026-09-01_2026-09-30.pdf"` y `Cache-Control: private, no-store`.

Exige el mismo token de acceso que cualquier otra ruta. No hay enlaces públicos ni firmados: el archivo se genera para quien lo pide y no se guarda en el servidor.

## 2. Contenido del documento

Tamaño A4, tipografía Manrope incrustada (licencia abierta), con los colores de la app.

1. **Encabezado:** "Orbita", título "Reporte" y el periodo en el formato de la app (`1 sep – 30 sep 2026`).
2. **Por cada moneda** con movimientos, la principal primero, bajo su nombre (`Soles (S/)`):
   - Resumen: Ingresos, Gastos y Balance del periodo, con signo.
   - "Dónde gastas más": categoría, monto, porcentaje y barra horizontal con el color de la categoría.
   - "Tus fuentes de ingreso": igual, con categorías de ingreso.
3. **Pie:** la nota de la pantalla ("Las compras con tarjeta aparecen aquí solo cuando las marcas como pagadas. Las transferencias entre tus cuentas no cuentan como ingreso ni gasto."), fecha de generación y número de página.

Periodo sin movimientos: un PDF de una página con "No hay movimientos en este periodo", no un error.

**Los números salen del mismo `ReportsService`** que alimenta `GET /reports/summary`. El PDF no recalcula nada: solo dibuja. Así es imposible que la pantalla y el PDF muestren cifras distintas.

### Formato de textos

Idéntico a la sección 4 de `ORBITA_SPEC.md`:

| Dato | Formato | Ejemplo |
|---|---|---|
| Monto | símbolo + espacio + `#,##0.00` | `S/ 1,245.80` |
| Ingreso / egreso | signo + monto; el egreso con el signo menos U+2212 | `+S/ 3,500.00` · `−S/ 18.50` |
| Fecha | `d MMM yyyy`, mes en minúscula y sin punto | `2 oct 2026` |
| Mes | nombre con mayúscula inicial + año | `Septiembre 2026` |
| Porcentaje | un decimal | `37.2%` |

Se implementa en `common/money` y `common/dates` como funciones de formato en español, con las mismas pruebas que `Formatters.kt` de Android. Los nombres de los meses van en una tabla propia, sin depender de la configuración regional del servidor.

## 3. Generación

- Biblioteca **`pdfkit`**: genera el PDF directamente, sin navegador sin interfaz. Usa poca memoria, arranca al instante y no añade cientos de megas a la imagen.
- El documento se **transmite** a la respuesta mientras se genera, en lugar de construirlo entero en memoria.
- Al ser solo el resumen, el documento sale de unas pocas filas agregadas: su tamaño no depende de cuántos movimientos tenga el periodo.

No se usa una cola de trabajos: el resumen es un puñado de filas agregadas y se genera en milisegundos. Una cola añadiría Redis, un proceso trabajador y un flujo de "pedir, esperar, descargar" sin beneficio a este tamaño. El disparador para reconsiderarlo está en la sección 5.

## 4. Límites

Generar un PDF cuesta más CPU que una consulta normal, así que se acota:

| Límite | Valor de partida |
|---|---|
| Peticiones por usuario | 10 por hora |
| Generaciones simultáneas por réplica | 4; la siguiente espera hasta 5 s y, si no hay hueco, recibe `503` con `Retry-After` |
| Tiempo máximo de generación | 20 s |
| Rango máximo | El mismo de los reportes |

Si el cliente corta la conexión, la generación se cancela.

## 5. Cuándo pasar a generación en segundo plano

Solo si se cumple alguna de estas condiciones, medidas en producción:

- La generación supera los 5 s en el percentil 95.
- Se quiere enviar el PDF por correo o programar reportes mensuales.
- Se decide más adelante incluir el detalle de movimientos, con decenas de miles de filas.

En ese caso: cola de trabajos, almacenamiento de objetos con enlaces de descarga firmados y de corta vida, y un endpoint de estado. Queda fuera de este plan.

## 6. Seguridad

- El usuario solo obtiene sus propios datos: el servicio recibe el `userId` del token, igual que el reporte en JSON.
- Las descripciones y los nombres de categorías son texto del usuario. `pdfkit` los dibuja como texto, no los interpreta, así que no hay inyección de contenido activo. Se eliminan los caracteres de control antes de dibujar.
- El nombre del archivo se construye solo con fechas validadas, nunca con texto del usuario.
- No se guarda ninguna copia del PDF en el servidor.
- El PDF se genera sin metadatos que identifiquen al usuario más allá de lo que ya contiene.

## 7. Tareas

- [ ] Funciones de formato en español (`formatMoney`, `formatSignedMoney`, `formatDate`, `formatMonthYear`, `formatPercent`) con sus pruebas
- [ ] Incluir las fuentes Manrope en la imagen y registrarlas en `pdfkit`
- [ ] `ReportPdfRenderer`: recibe el resultado de `ReportsService` y escribe en un flujo
- [ ] Componentes de dibujo: encabezado, bloque resumen, ranking con barras con salto de página, pie con numeración
- [ ] Módulo `exports` con el endpoint, cabeceras y transmisión
- [ ] Límite de concurrencia, de peticiones, de filas y de tiempo
- [ ] Cancelación al cerrarse la conexión
- [ ] Documentar en OpenAPI la respuesta binaria

## 8. Pruebas

Unitarias (formato):

| Entrada | Esperado |
|---|---|
| `1245.8`, PEN | `S/ 1,245.80` |
| `250`, USD | `US$ 250.00` |
| `18.5`, PEN, egreso | `−S/ 18.50` (con U+2212) |
| `2026-10-02` | `2 oct 2026` |
| `2026-09-01` como mes | `Septiembre 2026` |
| `37.24` | `37.2%` |

Integración:

- El PDF de septiembre de la semilla se abre como PDF válido y su texto extraído contiene `S/ 4,200.00`, `S/ 2,148.60`, `+S/ 2,051.40`, las seis categorías de gasto y sus porcentajes.
- Las cifras del PDF coinciden con las de `GET /reports/summary` para el mismo periodo (se comparan en la misma prueba).
- Un periodo con dos monedas produce dos bloques, la principal primero.
- Un periodo vacío produce un PDF válido con el mensaje de vacío.
- El PDF no contiene ninguna lista de movimientos, y enviar `includeMovements` devuelve `400` (parámetro no admitido).
- Una descripción con caracteres especiales, acentos, emojis y 500 caracteres no rompe el documento ni desborda la página.
- Con el reloj fijado, dos generaciones del mismo reporte producen el mismo contenido.

e2e:

- Sin token: `401`.
- El usuario B no puede obtener datos de A por ninguna combinación de parámetros.
- La undécima petición en una hora devuelve `429`.
- Las cabeceras `Content-Type` y `Content-Disposition` son las esperadas.

Rendimiento:

- Con un usuario de 100,000 movimientos, el PDF de resumen de un año se genera por debajo del objetivo, y la memoria del proceso no crece con el tamaño del periodo.

## 9. Criterios de aceptación

- Desde Android se puede descargar y abrir el PDF del periodo que se ve en pantalla.
- Las cifras del PDF son idénticas a las de la pantalla.
- Diez generaciones simultáneas no degradan el resto de los endpoints.

## 10. Riesgos

| Riesgo | Mitigación |
|---|---|
| La generación de PDF satura la CPU y ralentiza todo lo demás | Límite de concurrencia por réplica, de peticiones por usuario y de tiempo |
| Textos largos o con caracteres raros desbordan el diseño | Recorte con puntos suspensivos y pruebas con casos extremos |
| La fuente no incluye algún símbolo (por ejemplo `−` o `€`) | Verificar la cobertura de Manrope para los 9 símbolos de moneda y el signo menos, con una prueba |
| El PDF y la pantalla muestran cifras distintas | Una sola fuente de datos (`ReportsService`) y una prueba que las compara |
