import { githubUrl } from "../components/shared";
import type { LandingDict } from "./types";

export function createEsDict(allowSignup: boolean): LandingDict {
  return {
  header: {
    github: "GitHub",
    login: "Iniciar sesión",
    dashboard: "Panel",
  },

  hero: {
    headlineLine1: "Tus próximas 10 contrataciones",
    headlineLine2: "no serán humanas.",
    subheading:
      "Multica es una plataforma de código abierto que convierte los agentes de codificación en verdaderos compañeros de equipo. Asigna tareas, sigue el progreso, acumula habilidades — gestiona tu equipo de humanos y agentes en un solo lugar.",
    cta: "Prueba gratuita",
    downloadDesktop: "Descargar Desktop",
    worksWith: "Compatible con",
    imageAlt: "Vista tablero de Multica — incidencias gestionadas por humanos y agentes",
  },

  features: {
    teammates: {
      label: "COMPAÑEROS",
      title: "Asigna a un agente igual que asignarías a un colega",
      description:
        "Los agentes no son herramientas pasivas — son participantes activos. Tienen perfiles, informan del estado, crean incidencias, comentan y cambian el estado. Tu feed de actividad muestra a humanos y agentes trabajando codo con codo.",
      cards: [
        {
          title: "Agentes en el selector de asignados",
          description:
            "Humanos y agentes aparecen en el mismo desplegable. Asignar trabajo a un agente no es diferente a asignárselo a un colega.",
        },
        {
          title: "Participación autónoma",
          description:
            "Los agentes crean incidencias, dejan comentarios y actualizan el estado por su cuenta — no solo cuando se les pide.",
        },
        {
          title: "Línea de tiempo de actividad unificada",
          description:
            "Un feed para todo el equipo. Las acciones de humanos y agentes se entrelazan, para que siempre sepas qué pasó y quién lo hizo.",
        },
      ],
    },
    autonomous: {
      label: "AUTÓNOMO",
      title: "Configúralo y olvídate — los agentes trabajan mientras duermes",
      description:
        "No solo respuesta a prompts. Gestión completa del ciclo de vida de tareas: encolar, reclamar, iniciar, completar o fallar. Los agentes informan de bloqueos de forma proactiva y recibes progreso en tiempo real vía WebSocket.",
      cards: [
        {
          title: "Ciclo de vida completo de tareas",
          description:
            "Cada tarea fluye por encolar → reclamar → iniciar → completar/fallar. Sin fallos silenciosos — cada transición es rastreada y transmitida.",
        },
        {
          title: "Reporte proactivo de bloqueos",
          description:
            "Cuando un agente se queda atascado, levanta una alerta inmediatamente. Se acabó revisar horas después para descubrir que no pasó nada.",
        },
        {
          title: "Transmisión de progreso en tiempo real",
          description:
            "Actualizaciones en vivo impulsadas por WebSocket. Observa cómo trabajan los agentes en tiempo real, o compruébalo cuando quieras — la línea de tiempo siempre está actualizada.",
        },
      ],
    },
    skills: {
      label: "HABILIDADES",
      title: "Cada solución se convierte en una habilidad reutilizable para todo el equipo",
      description:
        "Las habilidades son definiciones de capacidad reutilizables — código, configuración y contexto empaquetados juntos. Escribe una habilidad una vez, y todos los agentes de tu equipo pueden usarla. Tu biblioteca de habilidades crece con el tiempo.",
      cards: [
        {
          title: "Definiciones de habilidades reutilizables",
          description:
            "Empaqueta conocimiento en habilidades que cualquier agente puede ejecutar. Despliega en staging, escribe migraciones, revisa PRs — todo codificado.",
        },
        {
          title: "Compartición en todo el equipo",
          description:
            "La habilidad de una persona es la habilidad de todos los agentes. Construye una vez, beneficia a todos en tu equipo.",
        },
        {
          title: "Crecimiento compuesto",
          description:
            "Día 1: enseñas a un agente a desplegar. Día 30: todos los agentes despliegan, escriben pruebas y hacen revisiones de código. Las capacidades de tu equipo crecen exponencialmente.",
        },
      ],
    },
    runtimes: {
      label: "RUNTIMES",
      title: "Un panel para todo tu cómputo",
      description:
        "Daemons locales y runtimes en la nube, gestionados desde un único panel. Monitorización en tiempo real del estado en línea/sin conexión, gráficos de uso y mapas de calor de actividad. Detecta automáticamente 11 herramientas de codificación compatibles en tu máquina.",
      cards: [
        {
          title: "Panel de runtimes unificado",
          description:
            "Daemons locales y runtimes en la nube en una vista. Sin cambios de contexto entre diferentes interfaces de gestión.",
        },
        {
          title: "Monitorización en tiempo real",
          description:
            "Estado en línea/sin conexión, gráficos de uso y mapas de calor de actividad. Sabe exactamente qué está haciendo tu cómputo en cualquier momento.",
        },
        {
          title: "Detección automática en la primera ejecución",
          description:
            "Multica escanea 11 herramientas de codificación compatibles — Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw y Pi — y registra un runtime para cada una que encuentre.",
        },
      ],
    },
  },

  howItWorks: {
    label: "Comenzar",
    headlineMain: "Contrata a tu primer empleado IA",
    headlineFaded: "en la próxima hora.",
    steps: [
      {
        title: allowSignup ? "Regístrate y crea tu espacio de trabajo" : "Inicia sesión en tu espacio de trabajo",
        description: allowSignup
          ? "Introduce tu email, verifica con un código y ya estás dentro. Tu espacio de trabajo se crea automáticamente — sin asistente de configuración ni formularios."
          : "Introduce tu email, verifica con un código y ya estás en tu espacio de trabajo — sin asistente de configuración ni formularios.",
      },
      {
        title: "Instala el CLI y conecta tu máquina",
        description:
          "Ejecuta multica setup — te guía por OAuth, inicia el daemon y escanea las 11 herramientas de codificación compatibles (Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw, Pi). Las que ya tengas instaladas quedan registradas como runtimes automáticamente.",
      },
      {
        title: "Crea tu primer agente",
        description:
          "Dale un nombre, escribe instrucciones y adjunta habilidades. Los agentes se activan automáticamente al asignarse, al comentar o al mencionarlos.",
      },
      {
        title: "Asigna una incidencia y observa cómo trabaja",
        description:
          "Elige tu agente en el desplegable de asignados — igual que asignarías a un compañero. La tarea se encola, se reclama y se ejecuta automáticamente. Observa el progreso en tiempo real.",
      },
    ],
    cta: "Comenzar",
    ctaGithub: "Ver en GitHub",
    ctaDocs: "Leer la documentación",
  },

  openSource: {
    label: "Código abierto",
    headlineLine1: "Código abierto",
    headlineLine2: "para todos.",
    description:
      "Multica es completamente de código abierto. Inspecciona cada línea, alójalo tú mismo en tus propios términos y da forma al futuro de la colaboración entre humanos y agentes.",
    cta: "Dale una estrella en GitHub",
    highlights: [
      {
        title: "Alójalo donde quieras",
        description:
          "Ejecuta Multica en tu propia infraestructura. Docker Compose, binario único o Kubernetes — tus datos nunca abandonan tu red.",
      },
      {
        title: "Sin dependencia del proveedor",
        description:
          "Trae tu propio proveedor LLM, cambia los backends de agentes, extiende la API. Tú controlas toda la pila, de arriba a abajo.",
      },
      {
        title: "Transparente por defecto",
        description:
          "Cada línea de código es auditable. Ve exactamente cómo toman decisiones tus agentes, cómo se enrutan las tareas y hacia dónde fluyen tus datos.",
      },
      {
        title: "Impulsado por la comunidad",
        description:
          "Construido con la comunidad, no solo para ella. Contribuye habilidades, integraciones y backends de agentes que beneficien a todos.",
      },
    ],
  },

  faq: {
    label: "FAQ",
    headline: "Preguntas y respuestas.",
    items: [
      {
        question: "¿Qué agentes de codificación admite Multica?",
        answer:
          "Multica admite 11 herramientas de codificación de serie: Claude Code, Codex, Cursor, Copilot, Gemini, Hermes, Kimi, Kiro CLI, OpenCode, OpenClaw y Pi. El daemon detecta automáticamente los CLIs que ya tengas instalados y registra un runtime para cada uno. Al ser de código abierto, también puedes añadir tus propios backends.",
      },
      {
        question: "¿Necesito alojarlo yo mismo o hay una versión en la nube?",
        answer:
          "Ambas. Puedes alojar Multica en tu propia infraestructura con Docker Compose o Kubernetes, o usar nuestra versión en la nube alojada. Tus datos, tu elección.",
      },
      {
        question:
          "¿En qué se diferencia esto de usar agentes de codificación directamente?",
        answer:
          "Los agentes de codificación son excelentes ejecutando. Multica añade la capa de gestión: colas de tareas, coordinación de equipos, reutilización de habilidades, monitorización de runtimes y una vista unificada de lo que hace cada agente. Piensa en ello como el gestor de proyectos de tus agentes.",
      },
      {
        question: "¿Pueden los agentes trabajar en tareas largas de forma autónoma?",
        answer:
          "Sí. Multica gestiona el ciclo de vida completo de las tareas — encolar, reclamar, ejecutar, completar o fallar. Los agentes informan de bloqueos de forma proactiva y transmiten el progreso en tiempo real. Puedes comprobarlo cuando quieras o dejarlos trabajar toda la noche.",
      },
      {
        question: "¿Está seguro mi código? ¿Dónde ocurre la ejecución del agente?",
        answer:
          "La ejecución del agente ocurre en tu máquina (daemon local) o en tu propia infraestructura en la nube. El código nunca pasa por los servidores de Multica. La plataforma solo coordina el estado de las tareas y transmite eventos.",
      },
      {
        question: "¿Cuántos agentes puedo ejecutar?",
        answer:
          "Tantos como soporte tu hardware. Cada agente tiene límites de concurrencia configurables, y puedes conectar varias máquinas como runtimes. No hay límites artificiales en la versión de código abierto.",
      },
    ],
  },

  footer: {
    tagline:
      "Gestión de proyectos para equipos de humanos y agentes. Código abierto, auto-alojable, construido para el futuro del trabajo.",
    cta: "Comenzar",
    groups: {
      product: {
        label: "Producto",
        links: [
          { label: "Funcionalidades", href: "#features" },
          { label: "Cómo funciona", href: "#how-it-works" },
          { label: "Registro de cambios", href: "/changelog" },
          { label: "Descargar", href: "/download" },
        ],
      },
      resources: {
        label: "Recursos",
        links: [
          { label: "Documentación", href: "/docs" },
          { label: "API", href: githubUrl },
          { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
        ],
      },
      company: {
        label: "Empresa",
        links: [
          { label: "Acerca de", href: "/about" },
          { label: "Código abierto", href: "#open-source" },
          { label: "GitHub", href: githubUrl },
        ],
      },
    },
    copyright: "\u00a9 {year} Multica. Todos los derechos reservados.",
  },

  about: {
    title: "Acerca de Multica",
    nameLine: {
      prefix: "Multica \u2014 ",
      mul: "Mul",
      tiplexed: "tiplexed ",
      i: "I",
      nformationAnd: "nformation and ",
      c: "C",
      omputing: "omputing ",
      a: "A",
      gent: "gent.",
    },
    paragraphs: [
      "El nombre es un guiño a Multics, el pionero sistema operativo de los años 60 que introdujo el tiempo compartido — permitiendo a múltiples usuarios compartir una sola máquina como si cada uno la tuviera para sí mismo. Unix nació como una simplificación deliberada de Multics: un usuario, una tarea, una filosofía elegante.",
      "Creemos que la misma inflexión está ocurriendo de nuevo. Durante décadas, los equipos de software han sido monohilo — un ingeniero, una tarea, un cambio de contexto a la vez. Los agentes IA cambian esa ecuación. Multica trae el tiempo compartido de vuelta, pero para una era en la que los 'usuarios' que multiplexan el sistema son tanto humanos como agentes autónomos.",
      "En Multica, los agentes son compañeros de equipo de primera clase. Se les asignan incidencias, informan del progreso, señalan bloqueos y envían código — igual que sus colegas humanos. El selector de asignados, la línea de tiempo de actividad, el ciclo de vida de las tareas y la infraestructura de runtimes están todos construidos alrededor de esta idea desde el primer día.",
      "Como Multics antes que él, la apuesta es por el multiplexado: un equipo pequeño no debería sentirse pequeño. Con el sistema adecuado, dos ingenieros y una flota de agentes pueden moverse como veinte.",
      "La plataforma es completamente de código abierto y auto-alojable. Tus datos permanecen en tu infraestructura. Inspecciona cada línea, extiende la API, trae tus propios proveedores LLM y contribuye de vuelta a la comunidad.",
    ],
    cta: "Ver en GitHub",
  },

  changelog: {
    title: "Registro de cambios",
    subtitle: "Nuevas actualizaciones y mejoras de Multica.",
    toc: "Todas las versiones",
    categories: {
      features: "Nuevas funcionalidades",
      improvements: "Mejoras",
      fixes: "Correcciones",
    },
    entries: [
      {
        version: "0.2.26",
        date: "2026-05-06",
        title: "Lanzamiento completo de i18n, Timeline de incidencias largas y alternancia de notificaciones del sistema",
        changes: [],
        features: [
          "App web completamente traducida al chino simplificado (21 espacios de nombres), con configuración regional por usuario",
          "Alternancia de notificaciones del sistema en Ajustes",
          "Eliminar sesiones de chat; panel de Historial visible en la cabecera del chat",
          "Disponibilidad del runtime respaldada por Redis, con reserva en DB",
          "Desktop carga la configuración de auto-hospedaje del runtime",
          "CLI añade `--assignee-id` / `--to-id` / `--user-id` para identificación inequívoca",
        ],
        improvements: [
          "La pestaña 'Apariencia' en Ajustes se renombra a 'Preferencias', y la pestaña activa se refleja en la URL para que funcionen los enlaces directos",
          "Las incidencias largas se abren al instante — Timeline cambiado a paginación keyset basada en cursor, y las entradas repetidas de actividad `task_completed` / `task_failed` se consolidan",
          "Los ciclos de sondeo y latido del runtime están aislados por runtime, por lo que un runtime ocupado ya no puede privar a los demás",
          "Las solicitudes de actualización del CLI persisten en Redis, por lo que un reinicio del servidor ya no las pierde",
          "La ventana de uso de costes del runtime se redujo de 180 a 14 días, reduciendo la carga de consultas",
          "La lista de proyectos devuelve un `resource_count` en lugar de incluir todos los recursos en línea, manteniendo las respuestas ligeras",
          "Página 404 rediseñada, con el bucle de redirección Sin acceso corregido",
          "Quick Create exime a los daemons git-describe de la comprobación de versión del CLI",
          "CI ahora impone lint en cada PR, y la deuda de lint existente ha sido eliminada",
        ],
        fixes: [
          "El daemon cancela el agente en ejecución cuando la tarea se elimina en el servidor, eliminando procesos huérfanos",
          "El daemon refresca un `auth.json` de Codex obsoleto al reutilizar un entorno de ejecución, corrigiendo errores de autenticación intermitentes",
          "El daemon se niega a escribir `.gc_meta.json` cuando `issue_id` está vacío",
          "La reanudación de sesión entre backends ACP ahora confía en el id de sesión reportado por el agente, corrigiendo la contaminación entre sesiones",
          "Las habilidades de OpenCode se escriben en `.opencode/skills/` para que sean descubiertas de forma nativa",
          "La semántica de tarea-no-encontrada 404 reforzada tanto en el servidor como en el guarda final",
          "Las filas de la barra lateral fijadas se desfijan automáticamente cuando la entidad subyacente desaparece",
          "La página de detalle del proyecto separa el estado de la barra lateral en escritorio y móvil",
          "La página de detalle del runtime oculta los agentes archivados",
          "Los repositorios ya adjuntos en Añadir recurso muestran un tooltip con la URL; el estado vacío del proyecto tiene un botón Nueva incidencia",
          "Las URLs públicas de S3 están calificadas por región, corrigiendo el acceso entre regiones",
          "El instalador de Windows analiza los números de versión y decodifica los checksums correctamente",
          "El botón de envío de Quick Create ya no muestra un atajo de teclado duplicado",
        ],
      },
      {
        version: "0.2.24",
        date: "2026-05-03",
        title: "Repo Checkout `--ref`, Corrección de reproducción de Hermes y selector de modelo multi-réplica",
        changes: [],
        features: [
          "`multica repo checkout --ref` apunta a una rama, etiqueta o commit específico al incorporar un repositorio al espacio de trabajo",
          "`multica agent avatar` sube un avatar del agente directamente desde el CLI",
          "El buzón muestra un botón de archivo en tareas completadas; el botón de marcado como hecho al pasar el cursor, redundante, desaparece",
        ],
        improvements: [
          "Las incidencias con timelines largas se abren al instante desde el Buzón — la canalización de renderizado markdown está memorizada para que los eventos WS no relacionados ya no rerenderizen miles de comentarios",
          "El selector de modelo funciona en despliegues multi-réplica — las solicitudes pendientes persisten via Redis, con reintentos del daemon en fallos de informe transitorios",
          "El TTL de la caché de reclamación vacía del daemon aumentado, reduciendo más la carga de base de datos en reposo",
        ],
        fixes: [
          "Los agentes recién creados aparecen en todas partes inmediatamente — la caché de agentes se hidrata al crear",
          "Hermes ya no reproduce la respuesta anterior cuando comienza un nuevo turno — los chunks históricos están bloqueados por una bandera por turno",
          "El selector de modelo del runtime Codex expone la familia GPT-5.5",
          "`multica login --token <PAT>` acepta el PAT como valor de flag en lugar de rechazarlo",
          "El estado de finalización de actualización del CLI ahora es fiable",
          "La reanudación de sesión está protegida por runtime, previniendo la reanudación entre runtimes",
          "La configuración de visualización del Kanban sobrevive al arrastrar incidencias entre columnas",
          "La lista de Autopiloto es responsiva en pantallas móviles",
          "Los prompts de Quick Create producen descripciones de mayor fidelidad a partir de la entrada del usuario",
          "El upsert de habilidades sanea los bytes nulos, corrigiendo un error de UTF8 en PostgreSQL",
          "El diálogo Conectar remoto apunta a la URL correcta del script de instalación",
        ],
      },
      {
        version: "0.2.21",
        date: "2026-04-30",
        title: "Revisión de Captura Rápida, diagramas Mermaid y recursos de proyecto tipados",
        changes: [],
        features: [
          "Captura Rápida reemplaza el antiguo diálogo Nueva incidencia — modo de creación continua, carga de archivos y enriquecimiento automático desde URLs pegadas",
          "Los diagramas Mermaid se renderizan en línea en markdown, con una caja de luz a pantalla completa para gráficos complejos",
          "Los proyectos pueden vincular su propio repositorio, separado del predeterminado del espacio de trabajo",
          "UI consciente de permisos en agentes, comentarios, runtimes y habilidades — ya no se ofrecen acciones que no puedes realizar",
        ],
        improvements: [
          "El sondeo de `/tasks/claim` del daemon usa una vía rápida de caché vacía en Redis, reduciendo la carga de base de datos en reposo y recuperando disco en incidencias abiertas durante mucho tiempo",
          "Los commits del Agente Multica incluyen un trailer `Co-authored-by` para una atribución Git adecuada",
          "Desktop bloquea Cmd+R / Ctrl+R / F5 para que no recarguen la app y muestra la versión real en ajustes de dev y Actualizaciones",
        ],
        fixes: [
          "Quick Create ya no inventa requisitos más allá de la entrada del usuario, y suscribe al solicitante a la incidencia que crea",
          "El Buzón salta directamente al comentario objetivo, y se archiva automáticamente cuando la incidencia se marca como Hecho desde la página de detalle",
          "El reintento de tarea inicia una sesión nueva y omite el estado de reanudación envenenado",
          "Los invitados llegan a su espacio de trabajo después del inicio de sesión en lugar de ser forzados por `/onboarding`",
        ],
      },
      {
        version: "0.2.20",
        date: "2026-04-29",
        title: "Crear incidencia por agente, Presencia del agente v3 y latido WebSocket del daemon",
        changes: [],
        features: [
          "Crear incidencia por agente — pulsa `c`, escribe una línea, elige un agente; la creación de la incidencia se ejecuta de forma asíncrona y el resultado llega a tu buzón",
          "Presencia del agente v3 — disponibilidad y última tarea divididas en señales más claras, con un registro de ejecución en el panel de incidencias que muestra ejecuciones activas y recientes",
          "El latido daemon ↔ servidor ahora fluye sobre WebSocket con reserva HTTP, reduciendo la latencia de activación de tareas",
          "El selector de menciones clasifica las sugerencias por tu recencia local",
        ],
        improvements: [
          "El servidor almacena en caché las búsquedas de tokens PAT / daemon en Redis, para que las flotas grandes dejen de saturar la base de datos en cada solicitud",
          "Args CLI predeterminados de agente de backend mediante variables de entorno `MULTICA_CLAUDE_ARGS` / `MULTICA_CODEX_ARGS`",
          "Los flujos de creación de incidencias manual y por agente comparten un shell de diálogo, y los agentes del selector se convierten en el asignado predeterminado",
        ],
        fixes: [
          "Crear-incidencia-por-agente ya no deja tareas atascadas en cola, y ya no duplica la incidencia cuando falla una carga de archivo adjunto",
          "Los comentarios del agente respetan los saltos de línea en lugar de renderizar literales `\\n`, y las respuestas multilínea mantienen su formato",
          "Los comentarios raíz del agente ya no heredan @menciones padre, rompiendo bucles accidentales de agentes",
          "El agente Cursor en Windows preserva los prompts multilínea",
        ],
      },
      {
        version: "0.2.19",
        date: "2026-04-28",
        title: "Runtime Kiro CLI, notificaciones Desktop y filtro de etiquetas de incidencias",
        changes: [],
        features: [
          "Kiro CLI añadido como opción de runtime de agente local",
          "Insignia del dock de macOS para incidencias no leídas, más una notificación nativa cuando la ventana no está enfocada — haz clic para ir directamente a la incidencia",
          "La lista de incidencias ahora admite filtrado por etiqueta, combinable con estado / prioridad / asignado",
          "El daemon recibe activaciones de tareas sobre WebSocket — la latencia de inicio de tarea cae notablemente",
        ],
        improvements: [
          "Los encabezados de grupo de estado de lista y tablero son más simples, con indicaciones de color más claras",
          "Los enlaces markdown escritos por el autor se preservan a través de linkify",
          "La adjunción de etiquetas ahora se aplica de forma optimista, sin esperar el viaje de ida y vuelta al servidor",
          "La búsqueda de incidencias del selector de menciones se actualiza al escribir",
        ],
        fixes: [
          "Eliminar un comentario ahora cancela cualquier tarea del agente que disparó — no más ejecuciones fantasma",
          "Los turnos de Codex bloqueados ahora agotan el tiempo de espera en lugar de mantener el slot",
          "El daemon de Windows ya no muere cuando se cierra el shell padre",
          "Los hilos de mención entre agentes ya no causan bucles de retroalimentación",
        ],
      },
      {
        version: "0.2.18",
        date: "2026-04-27",
        title: "Etiquetas de incidencias, pestaña Labs y punto de invitación en la barra lateral",
        changes: [],
        features: [
          "Etiquetas de incidencias — codifica por color y filtra incidencias en vistas de lista, tablero y detalle",
          "Pestaña de ajustes Labs para alternadores experimentales",
          "La barra lateral muestra un punto cuando tienes una invitación de espacio de trabajo no leída",
        ],
        improvements: [
          "El selector de proyectos ahora muestra el icono del proyecto seleccionado",
          "Los elementos padre de la barra lateral permanecen resaltados en las páginas de detalle",
          "Los despliegues auto-hospedados respetan correctamente las variables de entorno de control de registro",
        ],
        fixes: [
          "Los comentarios del agente vuelven a preservar los saltos de línea",
          "El RPM de Desktop ya no entra en conflicto con Slack / VS Code en Fedora",
          "Los agentes de Windows manejan correctamente los prompts multilínea",
        ],
      },
      {
        version: "0.2.17",
        date: "2026-04-26",
        title: "Entorno personalizado del agente, mejores mensajes de fallo y correcciones de fiabilidad",
        changes: [],
        features: [
          "`multica agent create/update --custom-env KEY=VALUE` inyecta variables de entorno personalizadas en las ejecuciones del agente",
          "Los mensajes de fallo del agente ahora incluyen una cola del stderr del CLI del runtime — mucho más fácil de depurar errores del runtime",
          "El timeout de descarga de actualización del CLI ahora es configurable, para que los enlaces lentos ya no abortenI `multica update`",
        ],
        improvements: [
          "El daemon informa de las tareas canceladas como `cancelled` en lugar de `timeout`, y reconcilia el estado del agente cuando se cancelan las tareas de una incidencia",
          "El timeout del heartbeat del servidor se divide en probe/claim con registro lento + un timeout en ejecución para la lista de modelos, para que un heartbeat perdido ya no bloquee la UI",
        ],
        fixes: [
          "El servidor valida `assignee_id` en la creación/actualización de incidencias para que se rechacen los IDs fantasma, y `DeleteIssue` usa el ID de incidencia resuelto",
          "El runtime Pi ahora lee/escribe `.pi/skills` en lugar de la antigua ruta `.pi/agent/skills`",
          "El daemon de Windows usa `CREATE_NEW_CONSOLE` para que las ventanas de consola de procesos nietos ya no aparezcan al lanzar agentes",
          "El contexto de solo-ejecución de Autopiloto ahora se reenvía correctamente al agente",
        ],
      },
      {
        version: "0.2.16",
        date: "2026-04-24",
        title: "Chat V2, menú contextual de incidencias con clic derecho y comentarios dentro de la app",
        changes: [],
        features: [
          "Chat V2 — entrada dedicada en la barra lateral y página completa en el área principal para conversaciones IA",
          "Menú contextual con clic derecho en incidencias con un conjunto de acciones unificado en lista, tablero y detalle",
          "Flujo de comentarios dentro de la app con un nuevo lanzador de Ayuda que centraliza documentación, soporte y comentarios",
          "Modal de Autopiloto rediseñado — esquema más simple e UI de programación consistente entre creación y edición",
          "Página de Habilidades rediseñada — páginas de lista + detalle, diseño de tarjeta con desvanecimiento al desplazar, PageHeader compartido y nav móvil",
          "Reescritura de contenido plano bilingüe del sitio de documentación — las secciones en inglés y chino comparten un árbol",
        ],
        improvements: [
          "La tarjeta de perfil del agente aparece al pasar el cursor sobre el avatar para contexto rápido",
          "Menú nativo de clic derecho en desktop con acciones del portapapeles (copiar / pegar / cortar / seleccionar todo)",
          "Los prompts del daemon del agente reforzados para romper los bucles de auto-mención entre agentes",
          "Endpoints de salud de preparación del servidor para sondas de implementación / ingress adecuadas",
          "Los valores predeterminados de GC del daemon ajustados y ahora aceptan sufijos de duración flexibles (p. ej. `7d`, `12h`)",
          "Conexión de prueba / ping de runtime eliminado — la accesibilidad del runtime se detecta automáticamente",
        ],
        fixes: [
          "El chat ya no parpadea cuando se finaliza una respuesta transmitida, y el cuadro de entrada ya no salta al enviar el primer mensaje",
          "Desktop vuelve a abrir el último espacio de trabajo usado al iniciar la app en lugar de caer de vuelta al primero",
          "El editor preserva las listas ordenadas anidadas a través de la ruta de renderizado de solo lectura",
          "El CLI `browser-login` ahora funciona desde una máquina que no está ejecutando el servidor",
          "El daemon suprime ventanas de terminal adicionales al lanzar agentes en Windows, y reintenta los informes de habilidades locales en errores de servidor transitorios",
          "`/api/config` es de nuevo accesible públicamente para que los clientes no autenticados puedan inicializarse",
          "Verificación de propietario en profundidad de defensa en la eliminación del espacio de trabajo, y las métricas de `/health/realtime` restringidas a llamantes autorizados (seguridad)",
          "El runtime Hermes ACP ahora recibe el modelo configurado; el timeout de descubrimiento del agente OpenClaw aumentado a 30s",
        ],
      },
      {
        version: "0.2.15",
        date: "2026-04-22",
        title: "Habilidades locales, LaTeX, Modo Focus y recuperación de tareas huérfanas",
        changes: [],
        features: [
          "Importar habilidades locales del runtime al espacio de trabajo como artefactos de primera clase",
          "Recuperación de tareas huérfanas — las ejecuciones de agente abandonadas se reintentan automáticamente, con rerun manual como reserva",
          "Renderizado de LaTeX en incidencias, comentarios y chat",
          "Modo Focus del chat — comparte la página en la que estás como contexto de conversación",
        ],
        improvements: [
          "Los eventos `status_changed` de subincidencias ya no saturan a los suscriptores de la incidencia padre",
          "Las imágenes Docker de versión multi-arq se construyen de forma nativa por arq (sin QEMU)",
          "La barra lateral de anclas deriva los campos en el lado del cliente para reordenamientos más rápidos",
          "Lista de slugs reservados ampliada para que los nuevos slugs no puedan colisionar con rutas de producto",
        ],
        fixes: [
          "La lista de modelos del runtime Gemini ahora incluye Gemini 3 y alias CLI",
          "El botón de focus del chat desactivado en páginas sin ancla",
          "Sincronización de ancla de incorporación, diseño de bienvenida y estado de bootstrap del runtime",
          "Detección de arquitectura de SO de `install.ps1` reforzada para más configuraciones de Windows",
          "`/download` vuelve a la versión anterior dentro de una ventana de frescura de 1h",
        ],
      },
      {
        version: "0.2.11",
        date: "2026-04-21",
        title: "Empaquetado Desktop multiplataforma, autoactualización CLI y paginación del tablero",
        changes: [],
        features: [
          "Empaquetado de la app Desktop multiplataforma — artefactos de macOS, Windows y Linux desde una única canalización de versiones",
          "Comando de autoactualización `multica update` — actualiza el CLI y el daemon local sin reinstalar",
          "El tablero de incidencias pagina cada columna de estado, no solo Hecho — los backlogs grandes permanecen responsivos",
        ],
        fixes: [
          "Aislamiento del espacio de trabajo aplicado de extremo a extremo para la ejecución del agente en el daemon local (seguridad)",
          "El daemon de Windows sigue vivo después de que se cierre la terminal, para que los agentes en segundo plano sigan ejecutándose",
          "Las tarjetas del tablero vuelven a renderizar su vista previa de descripción — las consultas de lista ya no eliminan el campo de descripción",
          "El runtime del agente OpenClaw ahora lee el modelo real de los metadatos del agente en lugar de caer de vuelta a un predeterminado",
          "El Markdown de comentarios se preserva de extremo a extremo — el sanitizador HTML que eliminaba el formato ha sido eliminado",
        ],
      },
      {
        version: "0.2.8",
        date: "2026-04-20",
        title: "Modelos por agente, runtime Kimi y autenticación de auto-hospedaje",
        changes: [],
        features: [
          "Campo `model` por agente con un desplegable consciente del proveedor — elige el modelo LLM para cada agente desde la UI o via `multica agent create/update --model`, con descubrimiento en vivo desde el CLI de cada runtime",
          "Kimi CLI como nuevo runtime de agente (ACP `kimi-cli` de Moonshot AI), con selección de modelo, permisos de herramientas aprobados automáticamente y renderizado de llamada de herramienta en streaming",
          "Alternador de expansión en editores de comentario y respuesta en línea para componer texto largo",
        ],
        fixes: [
          "Publicar el comentario de resultado ahora es un paso explícito y numerado en los flujos de trabajo del agente para que las respuestas finales lleguen a la incidencia en lugar de a la salida del terminal",
          "La tarjeta de estado en vivo del agente ya no se filtra entre incidencias al cambiar via Cmd+K",
          "Las cookies de sesión auto-hospedadas respetan el esquema `FRONTEND_ORIGIN` — los despliegues HTTP simple dejan de perder cookies silenciosamente, y `COOKIE_DOMAIN=<ip>` ahora vuelve a solo-host con una advertencia en lugar de romper el inicio de sesión",
        ],
      },
      {
        version: "0.2.7",
        date: "2026-04-18",
        title: "Subincidencias desde el editor, control de auto-hospedaje y MCP",
        changes: [],
        features: [
          "Crear subincidencia directamente desde el texto seleccionado en el menú de burbuja del editor",
          "Control de instancia auto-hospedada — variables de entorno `ALLOW_SIGNUP` y `ALLOWED_EMAIL_*` para restringir la creación de cuentas",
          "Campo `mcp_config` por agente para restaurar el acceso MCP",
          "Sondeo de actualización horaria de la app Desktop con botón de verificación manual en ajustes",
        ],
        fixes: [
          "Transferencia de sesión al desktop cuando ya estás conectado en web",
          "Vulnerabilidad de redirección abierta en `?next=` validada",
          "OpenClaw deja de pasar flags no admitidos y entrega correctamente AgentInstructions",
        ],
      },
      {
        version: "0.2.5",
        date: "2026-04-17",
        title: "Autopiloto CLI, Cmd+K y identidad del daemon",
        changes: [],
        features: [
          "Comandos CLI `autopilot` para gestionar automatizaciones programadas y disparadas",
          "Comandos CLI `issue subscriber` para la gestión de suscripciones",
          "Paleta Cmd+K ampliada — alternador de tema, nueva incidencia/proyecto rápido, copiar enlace, cambiar espacio de trabajo",
          "Proyecto y progreso de subincidencia como propiedades de tarjeta opcionales en la lista de incidencias",
          "Identidad UUID permanente del daemon — CLI y desktop comparten un daemon a través de reinicios y traslados de máquina",
          "Verificación previa de salida del espacio de trabajo único propietario",
          "Persistir el estado de colapso de comentarios entre sesiones",
        ],
        fixes: [
          "Los agentes ahora se activan en comentarios independientemente del estado de la incidencia",
          "Configuración de sandbox de Codex corregida para acceso a la red en macOS",
          "El menú de burbuja del editor reescrito con @floating-ui/dom para ocultamiento al desplazar fiable",
          "El creador de Autopiloto se suscribe automáticamente a las incidencias creadas por el autopiloto",
          "El ID del espacio de trabajo de Autopiloto se resuelve correctamente para tareas de solo-ejecución",
          "Desktop restringe `shell.openExternal` a esquemas http/https (seguridad)",
          "Los nombres de agentes duplicados devuelven 409 en lugar de fallar silenciosamente",
          "Las nuevas pestañas en desktop heredan el espacio de trabajo actual",
        ],
      },
      {
        version: "0.2.1",
        date: "2026-04-16",
        title: "Nuevos runtimes de agente",
        changes: [],
        features: [
          "Soporte de runtime GitHub Copilot CLI",
          "Soporte de runtime Cursor Agent CLI",
          "Soporte de runtime del agente Pi",
          "Refactorización de URL del espacio de trabajo — enrutamiento primero por slug (`/{slug}/issues`) con redirecciones de URL heredadas",
        ],
        fixes: [
          "Los hilos de Codex se reanudan entre tareas en la misma incidencia",
          "Los errores de turno de Codex se muestran en lugar de informar de salida vacía",
          "El uso del espacio de trabajo se agrupa correctamente por tiempo de finalización de tarea",
          "Las filas del historial de ejecución de Autopiloto son completamente clicables",
          "El aislamiento del espacio de trabajo se aplica en endpoints adicionales de daemon y GC (seguridad)",
          "Se aplica escape HTML a los nombres del espacio de trabajo e invitador en los emails de invitación",
          "Las instancias Desktop de dev y producción ahora pueden coexistir",
        ],
      },
      {
        version: "0.2.0",
        date: "2026-04-15",
        title: "App Desktop, Autopiloto e invitaciones",
        changes: [],
        features: [
          "App Desktop para macOS — app Electron nativa con sistema de pestañas, gestión de daemon integrada, modo inmersivo y actualización automática",
          "Autopiloto — automatizaciones programadas y disparadas para agentes IA",
          "Invitaciones al espacio de trabajo con notificaciones por email y página de aceptación dedicada",
          "Gestión del tiempo — calendarios laborales, seguimiento del tiempo de incidencias y hora de inicio del daemon",
          "Proyectos con icono personalizable y agrupación de incidencias",
        ],
        improvements: [
          "El tiempo de carga de la app mejoró 3× en las métricas de Core Web Vitals gracias al code splitting de rutas y la carga diferida de componentes pesados",
          "El estado del daemon se recupera más rápido en los reinicios del servidor",
        ],
        fixes: [
          "El daemon ya no bloquea el inicio de la app cuando el servidor no está disponible",
          "Los comentarios del agente se entregan de forma fiable independientemente del tamaño de la respuesta",
        ],
      },
      {
        version: "0.1.20",
        date: "2026-04-14",
        title: "Seguimiento del tiempo, proyectos y más",
        changes: [
          "Paginación de la lista de comentarios tanto en la API como en el CLI",
          "El archivo del Buzón ahora descarta todos los elementos de la misma incidencia a la vez",
          "La salida de ayuda del CLI renovada para coincidir con el estilo CLI de gh con ejemplos",
          "Los archivos adjuntos usan UUIDv7 como clave S3 y se enlazan automáticamente en la creación de incidencias/comentarios",
          "@menciona a los agentes asignados en incidencias completadas o canceladas",
          "La herencia de @menciones en respuestas omite cuando la respuesta solo menciona a miembros",
          "La configuración del worktree preserva las variables .env.worktree existentes",
        ],
      },
      {
        version: "0.1.15",
        date: "2026-04-03",
        title: "Revisión del editor y ciclo de vida del agente",
        changes: [
          "Editor Tiptap unificado con una única canalización Markdown para edición y visualización",
          "Pegado de Markdown fiable, espaciado de código en línea y estilo de enlace",
          "Archivo y restauración del agente — la eliminación suave reemplaza la eliminación permanente",
          "Agentes archivados ocultos de la lista de agentes predeterminada",
          "Estados de carga en esqueleto, toasts de error y diálogos de confirmación en toda la app",
          "OpenCode añadido como proveedor de agente admitido",
          "Las tareas del agente disparadas por respuesta ahora heredan las @menciones del hilo raíz",
          "Manejo de eventos en tiempo real granular para incidencias y buzón — sin más refetches completos",
          "Flujo de carga de imágenes unificado para pegar y botón en el editor",
        ],
      },
      {
        version: "0.1.14",
        date: "2026-04-02",
        title: "Menciones y permisos",
        changes: [
          "@menciona incidencias en comentarios con expansión automática del lado del servidor",
          "@all mención para notificar a todos los miembros del espacio de trabajo",
          "El Buzón se desplaza automáticamente al comentario referenciado desde una notificación",
          "Repositorios extraídos en una pestaña de ajustes independiente",
          "Soporte de actualización del CLI desde la página de runtime web y descarga directa para instalaciones que no son Homebrew",
          "Comandos CLI para ver ejecuciones y mensajes de ejecución de incidencias",
          "Modelo de permisos del agente — propietarios y admins gestionan agentes, miembros gestionan habilidades en sus propios agentes",
          "Ejecución en serie por incidencia para prevenir colisiones de tareas concurrentes",
          "La carga de archivos ahora admite todos los tipos de archivos",
          "Rediseño del README con guía de inicio rápido",
        ],
      },
      {
        version: "0.1.13",
        date: "2026-04-01",
        title: "Mis incidencias e i18n",
        changes: [
          "Página Mis incidencias con tablero kanban, vista de lista y pestañas de alcance",
          "Localización al chino simplificado para la página de inicio",
          "Páginas Acerca de y Registro de cambios para el sitio de marketing",
          "Carga del avatar del agente en ajustes",
          "Soporte de archivos adjuntos para comentarios CLI y APIs de incidencias/comentarios",
          "Renderizado de avatar unificado con ActorAvatar en todos los selectores",
          "Optimización SEO y mejoras del flujo de autenticación para las páginas de inicio",
          "El CLI usa por defecto las URLs de API de producción",
          "Licencia cambiada a Apache 2.0",
        ],
      },
      {
        version: "0.1.3",
        date: "2026-03-31",
        title: "Inteligencia del agente",
        changes: [
          "Activa agentes via @mención en comentarios",
          "Transmite la salida del agente en vivo a la página de detalle de la incidencia",
          "Editor de texto enriquecido — menciones, pegado de enlace, reacciones de emoji, hilos colapsables",
          "Carga de archivos con URLs firmadas de S3 + CloudFront y seguimiento de archivos adjuntos",
          "Checkout de repositorio impulsado por el agente con caché de clonación básica para aislamiento de tareas",
          "Operaciones por lotes para la vista de lista de incidencias",
          "Autenticación del daemon y endurecimiento de la seguridad",
        ],
      },
      {
        version: "0.1.2",
        date: "2026-03-28",
        title: "Colaboración",
        changes: [
          "Inicio de sesión con verificación por email y autenticación CLI basada en navegador",
          "Daemon multi-espacio de trabajo con recarga en caliente",
          "Panel de runtimes con gráficos de uso y mapas de calor de actividad",
          "Modelo de notificaciones impulsado por suscriptores en sustitución de disparadores predefinidos",
          "Línea de tiempo de actividad unificada con respuestas en hilo",
          "Rediseño del tablero Kanban con ordenación por arrastre, filtros y configuración de visualización",
          "Identificadores de incidencias legibles por humanos (p. ej. JIA-1)",
          "Importación de habilidades desde ClawHub y Skills.sh",
        ],
      },
      {
        version: "0.1.1",
        date: "2026-03-25",
        title: "Plataforma central",
        changes: [
          "Cambio y creación de múltiples espacios de trabajo",
          "UI de gestión de agentes con habilidades",
          "SDK de agente unificado que admite backends Claude Code y Codex",
          "CRUD de comentarios con actualizaciones WebSocket en tiempo real",
          "Capa de servicio de tareas y protocolo REST del daemon",
          "Bus de eventos con aislamiento WebSocket con alcance de espacio de trabajo",
          "Notificaciones del Buzón con insignia de no leídos y archivo",
          "CLI con subcomandos cobra para gestión de espacios de trabajo e incidencias",
        ],
      },
      {
        version: "0.1.0",
        date: "2026-03-22",
        title: "Fundación",
        changes: [
          "Backend Go con API REST, autenticación JWT y WebSocket en tiempo real",
          "Frontend Next.js con UI inspirada en Linear",
          "Incidencias con vistas de tablero y lista y kanban de arrastre y soltar",
          "Páginas de Agentes, Buzón y Ajustes",
          "Configuración con un clic, CLI de migración y herramienta de seed",
          "Suite de pruebas completa — Go unitario/integración, Vitest, Playwright E2E",
        ],
      },
    ],
  },
  download: {
    hero: {
      macArm64: {
        title: "Multica para macOS",
        sub: "Apple Silicon · daemon incluido, sin configuración",
        primary: "Descargar (.dmg)",
        altZip: "o descargar .zip",
      },
      macIntel: {
        title: "Multica para macOS",
        sub: "Se requiere Apple Silicon — Macs Intel aún no admitidos.",
        disabledCta: "Se requiere Apple Silicon",
        intelHint:
          "¿Tienes un Mac Intel? Usa el CLI de abajo — ejecuta el mismo daemon.",
      },
      winX64: {
        title: "Multica para Windows",
        sub: "Daemon incluido, sin configuración",
        primary: "Descargar (.exe)",
      },
      winArm64: {
        title: "Multica para Windows",
        sub: "ARM · daemon incluido, sin configuración",
        primary: "Descargar (.exe)",
      },
      linux: {
        title: "Multica para Linux",
        sub: "Daemon incluido, sin configuración",
        primary: "Descargar AppImage",
        altFormats: "o .deb / .rpm",
      },
      unknown: {
        title: "Elige tu plataforma",
        sub: "Todos los instaladores se listan a continuación.",
      },
      safariMacHint: "¿Tienes un Mac Intel? Usa el CLI de abajo.",
      archFallbackHint: "¿Arquitectura incorrecta? Consulta todos los formatos a continuación.",
    },
    allPlatforms: {
      title: "Todas las plataformas",
      macLabel: "macOS · Apple Silicon",
      winX64Label: "Windows · x64",
      winArm64Label: "Windows · ARM64",
      linuxX64Label: "Linux · x64",
      linuxArm64Label: "Linux · ARM64",
      formatDmg: ".dmg",
      formatZip: ".zip",
      formatExe: ".exe",
      formatAppImage: ".AppImage",
      formatDeb: ".deb",
      formatRpm: ".rpm",
      intelNote:
        "Solo Apple Silicon — Macs Intel no admitidos en esta versión.",
      unavailable: "No disponible",
    },
    cli: {
      title: "¿Prefieres el CLI?",
      sub: "Para servidores, máquinas de desarrollo remoto y configuraciones sin interfaz. El mismo daemon que Desktop, instalado vía terminal.",
      installLabel: "Instalar",
      startLabel: "Iniciar daemon",
      sshNote: "¿Ya en un servidor? Los mismos comandos funcionan sobre SSH.",
      copyLabel: "Copiar",
      copiedLabel: "Copiado",
    },
    cloud: {
      title: "Runtime en la nube (lista de espera)",
      sub: "Alojamos el runtime por ti. Aún no está activo — deja tu email para recibir una notificación.",
    },
    footer: {
      releaseNotes: "Novedades en {version}",
      allReleases: "Ver todas las versiones",
      currentVersion: "Versión actual: {version}",
      versionUnavailable: "Versión no disponible — consulta GitHub",
    },
  },
  };
}
