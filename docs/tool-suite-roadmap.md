# FileForge Tool Suite — Roadmap de Ejecución (MVP + DoD)

**Proyecto:** `wails-fileforge`  
**Estado del documento:** Histórico, reconciliado con `feat/v1.2-stabilization`
**Versión:** v1.2 (estado de ejecución actualizado)
**Fecha original:** 2026-03-27 · **Última reconciliación:** 2026-11-25

---

## 0) Portada breve

Este roadmap define una ejecución **por etapas**, con tareas accionables y criterios de salida verificables para llevar FileForge a un MVP consistente de suite de herramientas.  
La referencia de completitud es explícita: **MVP + DoD** (global y por herramienta).

---

## 1) Principios innegociables

0. **Política arquitectónica V1-only (Legacy = prohibido)**: toda capacidad activa debe vivir en contratos/jobs V1; no se admite coexistencia funcional con flujos legacy.
1. **MVP + DoD como criterio de “hecho”**: ninguna herramienta se considera cerrada si no cumple ambos.
2. **Desktop-first + offline-first**: operación local por defecto, sin dependencia de servicios cloud.
3. **Compatibilidad incremental**: releases pequeños por vertical slice, evitando big-bang.
4. **Errores normalizados y accionables**: mensajes consistentes para UI y debugging.
5. **Batch robusto**: soporte de estado `partial success` cuando hay mezcla de éxitos y fallos.
6. **Logs locales pragmáticos**: por tratarse de uso local, **no se exige ocultar paths** en logs técnicos.
7. **Sin deuda accidental**: contratos IPC versionados, responsabilidades separadas backend/frontend/tests/docs.

---

## 2) Matriz resumida de estado actual

> Referencia: estado funcional verificado contra el código en `feat/v1.2-stabilization` (2026-11-25).

| Área / Tool | Estado | Nota breve |
|---|---|---|
| Plataforma base Wails + Angular | Implementado | Wails v3.1, servicios tipados, bindings generados y frontend Angular 21 |
| Registro de tools y motor de jobs | Implementado | Un registro instanciado por el composition root y un motor con cola, límites por clase, cancelación y persistencia |
| `tool.image.crop` | Implementado | Single + batch, preview backend y salida no destructiva |
| `tool.image.annotate` | Implementado | Texto, flecha, rectángulo, **blur** y **redacción** en orden determinístico |
| `tool.pdf.merge` | Implementado | Respeta el orden de entrada, valida por archivo y reporta errores agregados |
| `tool.pdf.crop` | Implementado | Single + batch con `partial_success` y autoincremento de salida |
| `tool.doc.md_to_pdf` | Implementado | Header/footer opcional configurable; **single y batch** |
| `tool.doc.docx_to_pdf` | Implementado | Motor pure-Go incluido, single + batch; PDF/A con Ghostscript y selección de páginas con pdfcpu |
| `tool.video.convert` | Implementado | Presets, validación y progreso real por job |
| `tool.video.trim` | Implementado | Inicio/fin validados, salida atómica y progreso real |
| Batch transversal | Implementado | `partial_success`, reintentos acotados, recuperación al reinicio y recuperación de runs de receta |

La matriz original cubría el MVP. El catálogo actual tiene 20 tools: se agregaron
`tool.image.convert`, `tool.image.compress`, `tool.image.watermark`,
`tool.image.ocr`, `tool.image.background`, `tool.pdf.split`,
`tool.pdf.optimize`, `tool.pdf.ocr`, `tool.pdf.images`,
`tool.pdf.to_docx`, `tool.video.merge` y `tool.video.transcribe`
(`internal/registry`, manifiestos por dominio). Las recetas curadas
(`internal/recipes/service.go`) y las automatizaciones de carpeta
(`internal/automation`) son capas posteriores a este roadmap.

---

## 3) Alcance MVP (referencia de completitud)

Se considera MVP de suite cuando estén operativas estas capacidades con DoD aprobado. **Estado v1.2: todas entregadas**; el smoke E2E automatizado sigue pendiente (ver sección 7).

- [x] `tool.image.crop`
- [x] `tool.image.annotate` (texto, flecha, rectángulo, **blur, redacción**)
- [x] `tool.pdf.merge` (respeta orden recibido)
- [x] `tool.pdf.crop`
- [x] `tool.doc.md_to_pdf` (header/footer opcional configurable, single y batch)
- [x] `tool.doc.docx_to_pdf` (fidelidad estándar con motor pure-Go; alta fidelidad en backlog)
- [x] `tool.video.convert`
- [x] `tool.video.trim`
- [x] Ejecución batch con `partial success`
- [x] Retry policy v1 activa

### 3.1 Política arquitectónica obligatoria: **V1-only (Legacy = prohibido)**

**Objetivo**
- [x] Consolidar una plataforma única basada en jobs/contratos V1, sin rutas de ejecución paralelas legacy.

**Alcance**
- [x] Backend, frontend, bindings, contratos TS, registry y documentación deben quedar alineados al modelo V1-only.
- [x] Cualquier referencia legacy se considera deuda bloqueante de release, no deuda diferible.

**Dependencias**
- [x] Etapa 0 (base de contratos V1) operativa para permitir migración sin ambigüedad.

**Criterio rector de arquitectura**
- [x] Si existe alternativa entre mantener compatibilidad legacy o cerrar V1-only, prevalece V1-only.
- [x] No se aceptan feature flags permanentes para sostener endpoints o rutas legacy.

### 3.2 Inventario de legado activo a erradicar (auditoría)

- [x] APIs backend legacy: `ConvertFile`, `ConvertBatch`, `GetSupportedFormats`.
- [x] Flujo frontend legacy de `image-converter`.
- [x] Contratos TypeScript legacy en `frontend/src/app/services/wails.ts`.
- [x] Bindings Wails legacy asociados a APIs legacy.
- [x] Dual registry: old converter + tools V1 coexistiendo.

---

## 4) Roadmap por etapas (0..8 + lote Legacy Zero)

## Etapa 0 — Base operativa y contratos

**Objetivo**  
Establecer base técnica para ejecutar tools de forma uniforme (single + batch).

**Alcance**
- Contratos DTO V1 para tools/jobs/resultados.
- Estado normalizado de jobs (`queued`, `running`, `success`, `failed`, `partial success`, `cancelled`, `interrupted`).
- Estructura mínima de logging y error codes.

**Dependencias**
- Ninguna.

**Checklist**

### Backend
- [x] Definir `ToolManifestV1`, `JobRequestV1`, `JobResultV1` con campos obligatorios de estado y errores.
- [x] Definir catálogo de `error.code` (validación, runtime, dependencia, timeout, cancelación).
- [x] Exponer endpoints IPC V1 (`ListToolsV1`, `ValidateJobV1`, `RunJobV1`, `GetJobStatusV1`, `CancelJobV1`).

### Frontend
- [x] Crear estado unificado de jobs con mapeo 1:1 de estados backend.
- [x] Mostrar errores con traducción por `error.code` + mensaje técnico opcional.

### Tests
- [x] Unit tests de serialización/validación de DTOs.
- [x] Contract tests IPC (payload válido e inválido).

### Docs
- [x] Documentar diccionario de estados y códigos de error.

**Criterios de salida (gates)**
- UI y backend comparten mismos estados de job sin mapeos ambiguos.
- Todos los errores de validación/runtimes básicos quedan normalizados.

**Riesgos / mitigaciones**
- Riesgo: sobre-diseño temprano.  
  Mitigación: limitar V1 a campos estrictamente necesarios para MVP.

---

## Etapa 0.5 — Decommission legacy (mandatorio) + Gate **Legacy Zero**

**Objetivo**  
Eliminar completamente el legado activo y dejar la plataforma en modo **V1-only** antes de continuar releases funcionales.

**Alcance**
- Erradicar APIs/backend/frontend/bindings/registry legacy.
- Garantizar que imagen/home/catálogo operen únicamente sobre jobs y catálogo V1.

**Dependencias**
- Etapa 0 completada (contratos y endpoints V1 disponibles).

**Plan por lotes (erradicación legacy)**

### Lote A — Migración funcional UI
- [x] Migrar UI de imagen (`image-converter`) a ejecución por jobs V1 (`ValidateJobV1` / `RunJobV1` / `GetJobStatusV1`).
- [x] Eliminar rutas/handlers de UI que dependan de métodos legacy de conversión directa.

### Lote B — Catálogo y Home desacoplados de legacy
- [x] Desacoplar Home/catálogo de `GetSupportedFormats`.
- [x] Consumir catálogo de tools V1 (`ListToolsV1`) como fuente única.

### Lote C — Backend/API + bindings
- [x] Remover APIs backend legacy `ConvertFile`, `ConvertBatch`, `GetSupportedFormats`.
- [x] Regenerar bindings Wails para eliminar exports legacy asociados.
- [x] Verificar que frontend compile contra contratos V1 sin shims legacy.

### Lote D — Registry y contratos internos
- [x] Limpiar dual registry (old converter + tools V1) dejando un único registry/orchestrator V1.
- [x] Remover interface y wiring legacy de converter donde quede redundante con tools V1.

**Checklist de implementación por disciplina**

### Backend
- [x] Eliminar handlers/métodos legacy expuestos por App Service.
- [x] Eliminar rutas de ejecución legacy en service layer.
- [x] Dejar telemetría/logs apuntando solo a jobs V1.

### Frontend
- [x] Reemplazar invocaciones legacy en `frontend/src/app/services/wails.ts` por contratos V1-only.
- [x] Migrar componentes/rutas para no depender de flujo legacy `image-converter`.
- [x] Actualizar Home para catálogo V1-only.

### Tests
- [x] Agregar tests de no-regresión que fallen si reaparecen métodos legacy en llamadas IPC frontend.
- [x] Agregar tests backend que validen ausencia de endpoints legacy expuestos.
- [x] Ejecutar smoke V1 de imagen y catálogo post-decommission.

### Docs
- [x] Actualizar roadmap/guías para declarar explícitamente política V1-only.
- [x] Documentar decisiones de decommission y alcance de eliminación legacy.

**Criterios de salida (Gate final: Legacy Zero, medible)**
- [x] No existen referencias activas a `ConvertFile`, `ConvertBatch`, `GetSupportedFormats` en frontend ni backend.
- [x] No existen exports legacy en bindings Wails generados.
- [x] No existen rutas legacy activas en frontend (navegación) ni backend (IPC/service).
- [x] Tests de no-regresión V1 (unit/integration/smoke) pasan en alcance de decommission.

**Riesgos / mitigaciones**
- Riesgo: ruptura de UX durante migración de `image-converter`.  
  Mitigación: migración por lote A con smoke E2E específico antes de remover código legacy.
- Riesgo: dependencias ocultas en bindings/contratos TS.  
  Mitigación: remover API legacy + regenerar bindings + validación de referencias cruzadas.

---

## Etapa 1 — `tool.image.crop` (prioridad absoluta)

**Objetivo**  
Entregar primera herramienta de imagen utilizable de punta a punta.

**Alcance**
- Selección de área, presets de ratio, export en formatos soportados.

**Dependencias**
- Etapa 0.

**Checklist**

### Backend
- [x] Implementar `tool.image.crop` con validación de área y límites de imagen.
- [x] Soportar export seguro (nuevo archivo, no destructivo).

### Frontend
- [x] UI de crop con preset ratio (1:1, 4:3, 16:9, libre).
- [x] Preview de resultado antes de ejecutar.

### Tests
- [x] Unit tests de validación de coordenadas/ratio.
- [x] Integration test de crop sobre fixture real.
- [ ] Smoke frontend: flujo seleccionar → configurar → ejecutar → resultado (E2E real pendiente).

Nota Etapa 1: el smoke E2E real queda pendiente por restricción explícita de no incorporar framework E2E nuevo en esta etapa.

### Docs
- [x] Documentar formatos soportados, límites y errores frecuentes.

**Detalles implementados (Etapa 1):**
- Formatos de entrada: `jpg`, `jpeg`, `png`, `gif`, `webp`, `bmp`, `tiff`, `tif`.
- Formatos de salida: por defecto se preserva formato original; override opcional: `jpeg`, `png`, `webp`, `gif`, `tiff`.
- Coordenadas: origen `(0,0)` top-left.
- Límites: out-of-bounds devuelve error (sin clamp).
- Tamaño mínimo de área: `1x1`.
- Batch Etapa 1: misma área para todos los archivos del lote, con `partial success` si mezcla éxitos/fallos por ítem.
- Export no destructivo: autogenera `*_cropped` y resuelve colisiones con sufijos `-2`, `-3`, ...
- Preview: backend pixel-perfect (misma normalización EXIF + misma operación de crop que ejecución final).
- Restricción vigente: no se agregó framework E2E nuevo en esta etapa (smoke E2E transversal sigue pendiente).

**Criterios de salida (gates)**
- Resultado consistente para imágenes landscape/portrait.
- Error claro ante selección inválida o fuera de límites.

**Riesgos / mitigaciones**
- Riesgo: discrepancias de preview vs output final.  
  Mitigación: usar misma lógica de transform base para preview y ejecución.

---

## Etapa 2 — `tool.image.annotate` completo (incluye blur/redacción)

**Objetivo**  
Completar anotación base de imagen para casos de documentación y privacidad.

**Alcance**
- Texto, flecha, rectángulo, **blur**, **redacción**.

**Dependencias**
- Etapa 1.

**Checklist**

### Backend
- [x] Implementar modelo de operaciones de anotación con orden determinístico.
- [x] Soportar operaciones `text`, `arrow`, `rect`, `blur`, `redact`.
- [x] Asegurar export no destructivo.

### Frontend
- [x] Herramientas de dibujo/edición para los 5 tipos de anotación.
- [x] Selector de color, grosor y tipografía básica para texto/figuras.
- [x] UX clara para diferenciar blur vs redacción irreversible.

### Tests
- [x] Unit tests de parse/validación de operaciones de anotación.
- [x] Integration test de pipeline multi-anotación en orden.
- [x] Smoke de blur/redacción sobre área específica.

### Docs
- [x] Guía de uso de anotaciones y advertencia de redacción.

**Detalles implementados (Etapa 2):**
- Tool backend nuevo: `tool.image.annotate` con operaciones `text`, `arrow`, `rect`, `blur`, `redact`.
- Orden de operaciones: ejecución secuencial determinística en backend (v1), independiente del orden de input del usuario.
- `redact` v1: irreversible, con rectángulo sólido de color configurable; la ejecución exige una confirmación bloqueante.
- `blur`: intensidad configurable por operación en rango `0..100`.
- `text`: fuente única v1 (sans-serif), tamaño y color configurables, soporte multilinea (`\n`).
- `arrow` y `rect`: solo `stroke`, con opacidad configurable en `0..1`.
- Batch annotate v1: mismas operaciones para todos los archivos del lote.
- TODO explícito agregado en backend para futura variante por-archivo en batch.
- Preview annotate: backend pixel-perfect, usa exactamente el mismo pipeline que la ejecución final.
- UX v2 annotate: flujo principal de dibujo directo sobre imagen (overlay interactivo con selección de herramienta y cursor crosshair), conservando formularios como “Ajustes avanzados” para edición fina.
- UX redact: aviso visible “Blur vs redact” con la explicación de irreversibilidad y confirmación bloqueante antes de ejecutar cuando la solicitud incluye al menos una operación `redact` (diálogo accesible con foco en Cancelar, Tab atrapado, Escape cancela y atajos de la página suspendidos).
- “Run again”: el request aceptado se guarda en un único punto (`ToolRunController`) para las 20 herramientas, y cada página adopta el re-run con `adoptRunAgain`.
- Export no destructivo: autogenera `*_annotated` y resuelve colisiones con sufijos `-2`, `-3`, ...
- Restricción vigente: no se agregó framework E2E nuevo; smoke E2E transversal queda pendiente.

**Guía rápida de uso (`tool.image.annotate`):**
1. Seleccionar imagen (single) o lote (batch).
2. Agregar operaciones en el orden exacto de ejecución final.
3. Configurar opciones por operación (color, grosor, opacidad, intensidad blur, texto multilinea).
4. En caso de `redact`, leer el aviso de irreversibilidad y confirmar el diálogo bloqueante antes de ejecutar.
5. Validar y ejecutar job V1.
6. Revisar preview backend y outputs `*_annotated`.

**Advertencia de redacción (v1):**
- `redact` reemplaza permanentemente el área por un rectángulo sólido en el archivo de salida.
- No existe "deshacer" después de exportar; para ocultado reversible usar `blur`.

**Criterios de salida (gates)**
- Las cinco operaciones funcionan y se aplican en orden esperado.
- Redacción queda visualmente irreversible en output final.

**Riesgos / mitigaciones**
- Riesgo: ambigüedad funcional entre blur y redacción.  
  Mitigación: definición funcional explícita y ejemplos visuales en docs.

---

## Etapa 3 — `tool.pdf.merge` (orden estricto)

**Objetivo**  
Entregar merge de PDFs confiable y predecible.

**Alcance**
- Merge múltiple respetando **exactamente** el orden recibido desde UI.

**Dependencias**
- Etapa 0.

**Checklist**

### Backend
- [x] Implementar `tool.pdf.merge` leyendo lista de inputs en orden estable.
- [x] Validar PDFs inválidos/protegidos y devolver error por archivo.

### Frontend
- [x] UI con listado reordenable (drag/drop) y orden visible (índice).
- [x] Orden final visible en el `OutputPlanCard` antes de ejecutar (sin modal aparte desde v1.2).

### Tests
- [x] Test de orden: output debe reflejar secuencia de entrada.
- [x] Test de mezcla con archivo inválido para validar manejo de errores.

### Docs
- [x] Documentar regla de orden y comportamiento ante archivos corruptos.

**Detalles implementados (Etapa 3):**
- Orden estricto garantizado en `tool.pdf.merge`: el backend procesa y mergea en el mismo orden de `inputPaths` recibido desde UI, sin reordenamientos internos.
- Validación previa por archivo (modo agregado, no fail-fast): se validan todos los PDFs de entrada antes de mergear y se devuelve una lista `details.fileErrors[]` con error por archivo inválido/protegido.
- Distinción explícita de PDFs protegidos: los archivos con protección por contraseña devuelven `detail_code`/código específico `PDF_PROTECTED_INPUT`.
- Si hay mezcla de errores por tipo (ej. corrupto + protegido), el error agregado del job usa `PDF_INVALID_INPUTS` y conserva el detalle por archivo para diagnóstico.
- UI de merge mantiene un listado reordenable por drag con índice visible y botones de subir/bajar como alternativa accesible.
- El `OutputPlanCard` muestra el orden final y el destino resuelto antes de ejecutar; desde v1.2 ya no hay un modal de confirmación separado (`pdf-merge.spec.ts`).

**Criterios de salida (gates)**
- Orden final del PDF coincide 1:1 con orden enviado.
- Mensajes de error claros por input fallido.

**Riesgos / mitigaciones**
- Riesgo: librería altere orden internamente.  
  Mitigación: cobertura de tests de orden con fixtures canónicos.

---

## Etapa 4 — `tool.doc.md_to_pdf` con header/footer configurable

**Objetivo**  
Exportar Markdown a PDF con branding y metadatos básicos configurables.

**Alcance**
- Render markdown + estilos base + **header/footer opcional configurable**.

**Dependencias**
- Etapa 0.

**Checklist**

### Backend
- [x] Implementar `tool.doc.md_to_pdf` con opciones `header` y `footer` opcionales.
- [x] Validar plantillas/config inválidas con errores normalizados.

### Frontend
- [x] Form de configuración para header/footer (texto, alineación, habilitado).
- [x] Preview rápida de configuración seleccionada.

### Tests
- [x] Unit tests de opciones de render (mapeo de config y aplicación determinística de márgenes/estilo).
- [x] Integration test con y sin header/footer.

### Docs
- [x] Documentar opciones soportadas y ejemplos de configuración.

**Detalles implementados (Etapa 4):**
- Tool backend: `tool.doc.md_to_pdf` registrado en el catálogo, con ejecución **`single` y `batch`**. El batch se cerró en la fase 4D-2 del refactor backend (`b095259`); cada input planifica su propia salida y renderiza hasta 2 en paralelo.
- Motor de conversión: implementación en **Go puro**, sin runtime externo requerido en ejecución.
- Render v1 de Markdown→PDF en **A4 vertical**.
- Soporte v1 de contenido markdown con texto, imágenes y HTML inline/base (render aproximado para HTML complejo).
- Header/footer opcionales y configurables con:
  - `enabled`
  - `text`
  - `align` (`left|center|right`)
  - `font` (`helvetica|times|courier`)
  - `marginTop`
  - `marginBottom`
  - `color` en formato **`#RRGGBB`**
- Semántica explícita de márgenes (determinística en mm):
  - `header.marginTop`: distancia desde el borde superior de página al inicio de la línea del header.
  - `header.marginBottom`: separación reservada entre el fin del header y el inicio del contenido (participa en el margen superior efectivo del body).
  - `footer.marginTop`: separación reservada entre el fin del contenido y el inicio del footer (participa en el margen inferior efectivo del body).
  - `footer.marginBottom`: distancia desde el borde inferior de página al inicio de la línea del footer.
  - Margen efectivo del body:
    - `top = max(20, header.marginTop + 6 + header.marginBottom)`
    - `bottom = max(18, footer.marginTop + 6 + footer.marginBottom)`
    - (`6mm` = alto de línea de header/footer en v1)
- Placeholders permitidos en `text`: `{page}`, `{totalPages}`, `{date}`, `{fileName}`.
- Errores normalizados con `error.code` canónico y `detail_code` específico para casos de input/config/render.
- Output no destructivo con convención `*_md2pdf` y colisiones `-2`, `-3`, ...
- Imágenes en Markdown (v1):
  - relativas/locales: permitidas cuando se resuelven en disco y quedan embebidas en el PDF final;
  - remotas `http/https`: soportadas en render backend con descarga en Go puro, timeout/cancelación por contexto y embebido en el PDF para mantener portabilidad.
- Frontend nuevo para esta tool con formulario completo de header/footer, ejecución por jobs V1 (`ValidateJobV1` / `RunJobV1`) y preview rápida aproximada no pixel-perfect.
- Cobertura backend de render options ampliada con:
  - test unitario del plan de render (aplicación real de `marginTop`/`marginBottom` en header+footer);
  - test unitario de mapeo `JobRequestV1.options -> engine.RenderConfig` (márgenes, color, align, font);
  - integración manteniendo escenarios con y sin header/footer.

**Ejemplo de configuración (`JobRequestV1.options`):**

```json
{
  "outputPath": "/tmp/report.pdf",
  "header": {
    "enabled": true,
    "text": "{fileName} - {date}",
    "align": "left",
    "font": "helvetica",
    "marginTop": 2,
    "marginBottom": 1,
    "color": "#112233"
  },
  "footer": {
    "enabled": true,
    "text": "Page {page}/{totalPages}",
    "align": "right",
    "font": "courier",
    "marginTop": 1,
    "marginBottom": 2,
    "color": "#000000"
  }
}
```

**Límites conocidos v1 (Etapa 4):**
- `single` y `batch`; el batch planifica una salida por input y reporta `partial_success`.
- Preview frontend deliberadamente aproximada (no refleja layout final exacto del PDF).
- Render de HTML complejo dentro de Markdown se limita a un subset razonable de v1.
- Soporte remoto v1 acotado a formatos de imagen detectables como `png`, `jpeg` y `gif`.
- No se agregó smoke E2E nuevo para esta etapa (se mantiene restricción de framework).

**Criterios de salida (gates)**
- La opción deshabilitada no imprime header/footer.
- La opción habilitada respeta configuración ingresada.

**Riesgos / mitigaciones**
- Riesgo: variaciones de renderer por plataforma.  
  Mitigación: plantilla CSS acotada + dataset de smoke por SO.

---

## Etapa 5 — `tool.doc.docx_to_pdf` (rápido/aceptable)

**Objetivo**  
Habilitar conversión DOCX→PDF utilitaria para uso diario.

**Alcance**
- Implementación **rápida/aceptable** para documentos simples/medios.
- Declarar explícitamente que la **alta fidelidad** queda para fase futura.

**Dependencias**
- Etapa 0.

**Checklist**

### Backend
- [x] Integrar `tool.doc.docx_to_pdf` con motor seleccionado para entrega rápida.
- [x] Definir límites conocidos (tablas complejas, fuentes embebidas, layout avanzado).

### Frontend
- [x] Exponer la herramienta con badge “fidelidad estándar”.
- [x] Mostrar advertencia de límites antes de ejecutar.

### Tests
- [x] Suite de validación sobre set DOCX básico/intermedio.
- [x] Reporte de diferencias visuales para casos complejos.

### Docs
- [x] Documentar alcance “aceptable v1” y backlog de alta fidelidad.

**Detalles implementados (Etapa 5):**
- Tool backend nuevo: `tool.doc.docx_to_pdf` registrado en catálogo V1 con soporte `single` y `batch`.
- Motor de conversión: renderer pure-Go incluido en el binario, sin suite de oficina externa.
- PDF/A: el PDF renderizado se post-procesa con Ghostscript; si falta, la exportación falla con una guía de instalación en lugar de degradar a PDF simple.
- Selección de páginas: se renderiza el documento completo y se extraen las páginas pedidas con pdfcpu, validando el rango contra el total real.
- Inputs v1 restringidos a `.docx` (validación estricta en backend y frontend).
- Naming de salida no destructivo: `*_docx2pdf.pdf`, con colisiones `-2`, `-3`, ...
- Batch v1 continúa procesando ítems ante error; el estado agregado del job queda en `partial_success` cuando hay mezcla de éxitos/fallos.
- Rechazo de archivos protegidos: detección previa al renderizado por metadata DOCX (`word/settings.xml`), firma OLE/CFB y flag de cifrado ZIP.
- Fuentes: el renderer elige una fuente del sistema o una fuente latina embebida; si la resolución falla, retorna un error canónico explícito.
- Detail codes específicos con prefijo obligatorio `DOC_DOCX_TO_PDF_*` para validación, runtime, ejecución, protección y señales de fuentes.
- Frontend con:
  - badge y banner **“Standard fidelity”** siempre visibles antes de ejecutar,
  - sin diálogo de confirmación: la advertencia es informativa y no bloquea el job,
  - soporte de ejecución `single` + `batch`.
- Integración de la tool en Home y Tool Catalog.
- Testing de la versión v1.2:
  - fixtures DOCX generados dentro de los tests como paquetes OOXML mínimos (sin binarios en el repo),
  - cobertura de single, batch, PDF/A, selección de páginas, DOCX protegido, DOCX corrupto y errores de fuente (`internal/doc/engine/docx_to_pdf_test.go`, `internal/doc/engine/page_selection_test.go`),
  - specs de página para la nota de fidelidad y la ausencia de confirmación (`doc-docx-to-pdf.spec.ts`).
  - No hay snapshots visuales por página: la fidelidad estándar se comunica como límite conocido, no se compara pixel a pixel.

**Límites v1 explícitos (Etapa 5, actualizado en v1.2):**
- Fidelidad estándar: documentos complejos (tablas avanzadas, tipografías no disponibles, layouts muy densos), DTP, ajuste exacto de formas flotantes y parte del SmartArt pueden presentar diferencias visuales respecto al original.
- El renderer es `github.com/bobyeoh/docx2pdf-go v0.3.0`, una versión pre-1.0 fijada en `go.mod` y aislada detrás de `engine.DOCXRenderer`.
- PDF/A requiere Ghostscript; sin `gs` la exportación falla con un error de dependencia, nunca degrada a PDF simple.
- La detección de fuentes faltantes/sustituidas y protección de documento es **best-effort**: cuando hay señal detectable se devuelve error; no garantiza detección universal.
- La variante de alta fidelidad NO forma parte de esta etapa.

**Backlog alta fidelidad (post-MVP):**
- Pipeline DOCX→PDF de alta fidelidad con estrategia específica para tablas complejas, fuentes avanzadas/embebidas y layout complejo.
- Métricas comparativas y criterios de aceptación visual más estrictos que v1.

**Criterios de salida (gates)**
- Conversión estable en set de documentos de referencia v1.
- Limitaciones comunicadas sin ambigüedad en UI/docs.

**Riesgos / mitigaciones**
- Riesgo: expectativas de fidelidad no realistas.  
  Mitigación: etiquetado explícito + criterio objetivo de aceptación v1.

---

## Etapa 6 — `tool.pdf.crop`

**Objetivo**  
Agregar recorte PDF con configuración por páginas/rangos.

**Alcance**
- Selección de páginas/rangos y aplicación de recorte.

**Dependencias**
- Etapa 3 (dominio PDF base).

**Checklist**

### Backend
- [x] Implementar `tool.pdf.crop` con validación de rango y modelo de recorte por `preset + margins`.
- [x] Manejar páginas inválidas por archivo sin colapsar job completo.

### Frontend
- [x] UI de rangos (ej. `1-3,5,8-10`) con validación inmediata.
- [x] Resumen de alcance simple antes de ejecutar.

### Tests
- [x] Unit tests de parser de rangos en contexto `tool.pdf.crop`.
- [x] Integration test con rangos mixtos válidos/inválidos por archivo en batch.

### Docs
- [x] Guía de sintaxis de rangos y errores comunes.

**Detalles implementados (Etapa 6):**
- `tool.pdf.crop` mantiene modelo de recorte **`preset + margins`** (sin contrato nuevo por coordenadas).
- Validación global de sintaxis de rango (`pageSelection`) es **fail-fast**: si la sintaxis es inválida, el job se rechaza antes de ejecutar.
- En batch, rangos fuera de página por archivo ya no tumban todo el job: cada archivo devuelve resultado en `items[]` con `success/error` por ítem.
- En resultados batch mixtos, se devuelve además error agregado del job con `error.details.fileErrors[]` (`path`, `code`, `message`) para diagnóstico consolidado.
- Salida batch no destructiva robusta con autoincremento de colisiones: `*_cropped.pdf`, `*_cropped-2.pdf`, `*_cropped-3.pdf`, ...
- UI de `pdf-crop` muestra rango, preset/margins, cantidad de archivos y output en el `OutputPlanCard` antes de submit; desde v1.2 no hay modal de confirmación separado (`pdf-crop.spec.ts`).
- UI refleja correctamente `partial_success` y errores por archivo tanto a nivel item como agregado.

**Criterios de salida (gates)**
- Parser robusto de rangos y errores accionables.
- Output consistente en recortes parciales.

**Riesgos / mitigaciones**
- Riesgo: edge-cases de PDFs heterogéneos.  
  Mitigación: fixtures variados + fallback con error por página.

---

## Etapa 7 — Video MVP (`tool.video.convert` + `tool.video.trim`)

**Objetivo**  
Cubrir casos básicos de conversión y recorte de video.

**Alcance**
- Conversión con presets básicos.
- Trim con inicio/fin.

**Dependencias**
- Etapa 0 + disponibilidad de motor de video elegido.

**Checklist**

### Backend
- [x] Implementar `tool.video.convert` con presets `alta/media/baja`.
- [x] Implementar `tool.video.trim` con validación de timestamps.

### Frontend
- [x] Formularios de presets y parámetros mínimos de trim.
- [x] Progreso por job y estimación simple de duración.

### Tests
- [ ] Integration tests sobre clips cortos de referencia.
- [x] Tests de validación para timestamps inválidos.

### Docs
- [ ] Documentar formatos de entrada/salida soportados y límites.

**Estado v1.2:** `tool.video.convert` y `tool.video.trim` están implementados con
presets, validación de timestamps, progreso real por job y publicación atómica.
Los tests usan un runner de ffmpeg simulado, no clips reales; la integración con
clips de referencia y una guía dedicada de formatos siguen pendientes.

**Criterios de salida (gates)**
- Conversión y trim ejecutables en dataset smoke.
- Errores de formato/parámetro son entendibles y accionables.

**Riesgos / mitigaciones**
- Riesgo: variaciones de performance entre SO.  
  Mitigación: presets conservadores y límites de concurrencia.

---

## Etapa 8 — Batch hardening + resiliencia de reinicio

**Objetivo**  
Cerrar robustez transversal de ejecución batch y recuperación operacional.

**Alcance**
- Estado `partial success`.
- Reintentos automáticos para errores transitorios (máximo 3).
- Al reinicio: marcar jobs interrumpidos (`interrupted`).
- Reanudación automática explícitamente fuera de alcance v1.

**Dependencias**
- Etapas 1..7 operativas en forma mínima.

**Checklist**

### Backend
- [x] Implementar resolución de estado agregado batch (`success` / `failed` / `partial success`).
- [x] Implementar retry policy v1 con máximo 3 intentos para errores transitorios.
- [x] Persistir estado mínimo para detectar jobs interrumpidos al reiniciar.

### Frontend
- [x] Mostrar resumen batch con conteo de éxitos, fallos y reintentos.
- [x] Señalar explícitamente jobs `interrupted` tras reinicio.
- [x] Mensaje visible: “reanudación automática: roadmap futuro”.

### Tests
- [x] Integration test de lote mixto (éxitos/fallos) validando `partial success`.
- [x] Test de retries con fallo transitorio controlado hasta máximo 3.
- [x] Test de reinicio con marcado `interrupted`.

### Docs
- [x] Documentar retry policy v1 y límites.
- [x] Documentar recuperación al reinicio (sin auto-resume v1).

**Criterios de salida (gates)**
- Estado batch correcto en todos los escenarios mixtos.
- Retries no exceden 3 y solo aplican a errores transitorios.
- Reinicio marca interrumpidos de forma consistente.

**Riesgos / mitigaciones**
- Riesgo: clasificar mal errores transitorios/permanentes.  
  Mitigación: tabla explícita de clasificación + tests por tipo.

---

## 5) Error-handling transversal + Retry Policy v1

## 5.1 Modelo de errores v1

| Categoría | Código ejemplo | ¿Reintenta? | Notas |
|---|---|---|---|
| Validación | `VALIDATION_INVALID_INPUT` | No | Error determinístico de parámetros |
| Dependencia faltante | `RUNTIME_DEP_MISSING` | No | Requiere intervención/instalación |
| I/O temporal | `EXEC_IO_TRANSIENT` | Sí | Corte temporal de acceso/recurso |
| Timeout transitorio | `EXEC_TIMEOUT_TRANSIENT` | Sí | Reintento controlado |
| Formato no soportado | `UNSUPPORTED_FORMAT` | No | Debe cortar inmediato |
| Cancelación usuario | `CANCELLED_BY_USER` | No | Estado terminal |

## 5.2 Política de reintentos v1

- Máximo **3 intentos** por item de ejecución.
- Solo aplica a errores clasificados como **transitorios de ejecución**.
- Backoff recomendado: lineal corto (ej. 250ms, 500ms, 1000ms).
- Registrar en resultado final: cantidad de intentos y último error.

## 5.3 Estado de jobs batch

- `success`: todos los ítems exitosos.
- `failed`: todos los ítems fallidos.
- `partial success`: mezcla de éxitos y fallos (**obligatorio**).

## 5.4 Reinicio de aplicación

- Al iniciar, jobs no terminales previos se marcan como `interrupted`.
- La **reanudación automática NO forma parte de v1** (queda en backlog).

---

## 6) DoD global y DoD por herramienta

## 6.1 DoD global (obligatorio para declarar completitud MVP)

- [x] Tool registrada en catálogo con metadata y validaciones.
- [x] Flujo single E2E funcional (input → config → run → result).
- [x] Batch soportado o explícitamente no aplicable.
- [x] Errores normalizados por código + mensaje accionable.
- [ ] Cobertura mínima de tests (backend + frontend + smoke): backend y frontend completos; **smoke E2E automatizado pendiente**.
- [x] Documentación funcional y límites conocidos.
- [x] Logging técnico suficiente para diagnóstico local.

## 6.2 DoD por herramienta

| Tool | Checklist DoD específico |
|---|---|
| `tool.image.crop` | [x] selección válida [x] presets ratio [x] export no destructivo [x] tests de coordenadas |
| `tool.image.annotate` | [x] texto [x] flecha [x] rectángulo [x] blur [x] redacción [x] orden determinístico |
| `tool.pdf.merge` | [x] orden de entrada preservado [x] reordenamiento UI [x] errores por archivo |
| `tool.pdf.crop` | [x] parser de rangos [x] recorte por rango [x] errores accionables |
| `tool.doc.md_to_pdf` | [x] render markdown [x] header opcional [x] footer opcional [x] config aplicada [x] batch |
| `tool.doc.docx_to_pdf` | [x] conversión estándar [x] límites v1 documentados [x] etiqueta de fidelidad estándar [x] batch |
| `tool.video.convert` | [x] presets básicos [x] validación de formato [x] progreso visible |
| `tool.video.trim` | [x] inicio/fin válidos [x] salida estable [x] errores de timestamp claros |

---

## 7) Plan de testing (backend/frontend/e2e smoke)

| Capa | Objetivo | Entregables |
|---|---|---|
| Backend Unit | Validación y reglas de negocio | DTOs, validadores, parser de rangos, clasificación de errores |
| Backend Integration | Verificar ejecución real por tool | Fixtures por dominio + escenarios de fallo |
| Frontend Unit | Garantizar UX y estado coherente | Formularios, estados de job, render de resultados |
| E2E Smoke | Probar viabilidad end-to-end | 1 caso feliz + 1 caso de error por tool MVP |

Checklist mínimo transversal:
- [ ] Smoke single por cada tool MVP.
- [ ] Smoke batch mixto con `partial success`.
- [ ] Smoke de retries transitorios (3 intentos máximo).
- [ ] Smoke de reinicio con jobs `interrupted`.

El repositorio no incorpora un framework E2E. La verificación automática actual es
`task verify` (Go: 1.184 funciones de test) y `task verify:frontend` (Karma: 746
specs); los escenarios de batch, retry y reinicio están cubiertos por tests de Go,
no por smoke de navegador.

---

## 8) Plan de releases incremental

| Release | Contenido | Gate de salida | Estado v1.2 |
|---|---|---|---|
| R0 | Etapa 0 completada | Contratos V1 estables + estados de job consistentes | Entregado |
| R0-LZ | Etapa 0.5 decommission legacy | **Gate Legacy Zero** aprobado (sin APIs/rutas/exports legacy activos) | Entregado |
| R1 | `tool.image.crop` | DoD específico cumplido | Entregado; smoke E2E automatizado pendiente |
| R2 | `tool.image.annotate` | Blur/redacción operativos + tests de orden | Entregado |
| R3 | `tool.pdf.merge` | Orden estricto validado en tests | Entregado |
| R4 | `tool.doc.md_to_pdf` | Header/footer opcional configurable, single y batch | Entregado |
| R5 | `tool.doc.docx_to_pdf` v1 | Fidelidad estándar aceptable + límites documentados | Entregado con motor pure-Go; alta fidelidad en backlog |
| R6 | `tool.pdf.crop` | Rango + recorte robusto | Entregado |
| R7 | Video MVP | Convert + trim con progreso real | Entregado; smoke E2E automatizado pendiente |
| R8 | Batch hardening | `partial success` + retry v1 + interrupted tras reinicio | Entregado |

Notas de entrega:
- Cada release debe cerrar DoD global + DoD específico de lo incluido.
- No acumular features fuera de etapa para evitar descontrol de alcance.

---

## 9) Backlog post-MVP

- Reanudación automática de jobs interrumpidos (checkpoint/recovery real).
- `tool.doc.docx_to_pdf` alta fidelidad (layout complejo, tipografías avanzadas).
- Editor visual avanzado para PDF crop por canvas.
- Batch pipelines encadenados (multi-step).
- Presets de uso por dominio (legal, educación, marketing).
- Métricas locales enriquecidas de performance por tool.

---

## 10) Registro explícito de decisiones del usuario

| # | Decisión explícita | Estado en este roadmap |
|---|---|---|
| 1 | Política arquitectónica explícita: **V1-only (Legacy = prohibido)** | Incluida en principios + sección 3.1 + Etapa 0.5 |
| 2 | Eliminar legado activo (APIs, frontend, contratos TS, bindings, dual registry) | Inventario 3.2 + plan por lotes Etapa 0.5 |
| 3 | Gate final medible “Legacy Zero” obligatorio | Definido en criterios de salida de Etapa 0.5 + release R0-LZ |
| 4 | Referencia de completitud = MVP + DoD | Incluida en principios, alcance y gates |
| 5 | PDF merge respeta orden recibido | Incluida en Etapa 3 + DoD específico |
| 6 | Prioridad imagen: primero `tool.image.crop` | Etapa 1 prioritaria |
| 7 | `tool.image.annotate` incluye blur y redacción | Etapa 2 + DoD específico |
| 8 | `tool.doc.md_to_pdf` con header/footer opcional configurable | Etapa 4 + DoD específico |
| 9 | `tool.doc.docx_to_pdf` rápido/aceptable + alta fidelidad futura | Etapa 5 + backlog post-MVP |
| 10 | Batch con `partial success` en resultados mixtos | Etapa 8 + sección de estados |
| 11 | Reintentos automáticos máximo 3 para transitorios | Retry policy v1 |
| 12 | Al reiniciar: marcar interrumpidos; auto-resume a futuro | Etapa 8 + backlog post-MVP |
| 13 | No ocultar paths en logs por uso local | Principios innegociables |

---

## 11) Cierre operativo

Este documento queda como **registro histórico de la ejecución por etapas del MVP**, reconciliado con el estado v1.2. Las capacidades posteriores (recetas, automatizaciones, bandeja, historial, panel de capacidades) se documentan en `README.md` y `docs/differentiation-roadmap.md`.
La verificación de avance debe hacerse con checklists de etapa + DoD global/específico, no con percepción subjetiva de “ya está”.
