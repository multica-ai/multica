import type { LandingDict } from "./types";
import { frDemoCopy } from "./demo-fr";

export const frSiteCopy: Pick<LandingDict, "site" | "home" | "demo" | "meta"> = {
  demo: frDemoCopy,
  meta: {
    home: {
      title: "Multica — La gestion de projet pour les équipes humains + agents",
      description:
        "Plateforme au code source ouvert qui fait des agents de code de vrais coéquipiers. Confiez des tâches, suivez leur avancement, capitalisez les skills.",
      ogTitle: "Multica — La gestion de projet pour les équipes humains + agents",
      ogDescription: "Pilotez vos équipes humains + agents en un seul endroit.",
    },
    about: {
      title: "À propos",
      description:
        "Découvrez Multica — multiplexed information and computing agent. Une plateforme de gestion de projet au code source ouvert pour les équipes humains + agents.",
      ogTitle: "À propos de Multica",
      ogDescription: "L’histoire de Multica, et pourquoi nous construisons la gestion de projet des équipes humains + agents.",
    },
    changelog: {
      title: "Nouveautés",
      description: "Découvrez les nouveautés de Multica : fonctionnalités, améliorations et correctifs récents.",
      ogTitle: "Nouveautés | Multica",
      ogDescription: "Les dernières mises à jour et versions de Multica.",
    },
    contactSales: {
      title: "Contacter l’équipe commerciale",
      description: "Échangez avec l’équipe Multica pour déployer des workflows humains + agents dans votre entreprise.",
      ogTitle: "Contacter l’équipe commerciale — Multica",
      ogDescription: "Parlez-nous de votre équipe. Nous répondons sous trois jours ouvrés.",
    },
    download: {
      title: "Télécharger Multica",
      description: "Téléchargez Multica pour macOS, Windows ou Linux, ou installez la CLI sur vos serveurs et machines de dev distantes.",
      ogTitle: "Télécharger Multica",
      ogDescription: "Installez l’app de bureau Multica avec son démon intégré, ou la CLI pour vos serveurs et machines de dev distantes.",
    },
    licensing: {
      title: "Licence",
      description:
        "Quand utiliser Multica gratuitement et quand une licence commerciale est nécessaire, expliqué à travers des cas courants.",
      ogTitle: "Licence — Multica",
      ogDescription:
        "Gratuit en auto-hébergement au sein de votre organisation. Proposer Multica à d’autres en tant que service hébergé nécessite une licence commerciale.",
    },
    privacy: {
      title: "Politique de confidentialité",
      description: "Comment Multica collecte, utilise et protège les données personnelles sur multica.ai et Multica Cloud.",
      ogTitle: "Politique de confidentialité — Multica",
      ogDescription: "Comment Multica collecte, utilise et protège les données personnelles.",
    },
  },
  site: {
    nav: {
      product: "Produit",
      solutions: "Solutions",
      resources: "Ressources",
      customers: "Clients",
      pricing: "Tarifs",
      documentation: "Documentation",
      changelog: "Nouveautés",
      useCases: "Cas d’usage",
      blog: "Blog",
      download: "Télécharger",
      company: "Entreprise",
      about: "À propos",
      careers: "Carrières",
      press: "Presse et marque",
      contactSales: "Contacter les ventes",
      trust: "Confiance",
      security: "Sécurité",
      licensing: "Licence",
      privacy: "Confidentialité",
    },
    header: {
      home: "Accueil Multica",
      githubStars: "GitHub, {count} étoiles",
      logIn: "Se connecter",
      getStarted: "Commencer",
      dashboard: "Tableau de bord",
    },
    footer: {
      tagline: "L’espace de travail des équipes humains et agents.",
      navigation: "Pied de page",
      language: "Langue",
      copyright: "© {year} Multica. Tous droits réservés.",
      starTurns: "L’étoile a déjà tourné {count} fois",
    },
    localeSuggestion: {
      message: "Multica est disponible en français.",
      action: "Voir en français",
      dismiss: "Ignorer",
    },
    cta: {
      title: "Mettez votre premier agent au travail dès aujourd’hui",
      lead: "Connectez les agents que vous utilisez déjà et confiez-leur une première tâche.",
      getStarted: "Commencer",
      bookDemo: "Réserver une démo",
    },
  },
  home: {
    hero: {
      headlinePrefix: "Faites de ",
      headlineSuffix: " un coéquipier.",
      lead: "Un seul espace pour votre équipe et ses agents : confiez le travail, suivez-le, relisez-le. Vous gardez la main.",
      download: "Télécharger",
      downloadFor: "Télécharger pour {os}",
      alsoFor: "Aussi pour {platforms}",
      bookDemo: "Réserver une démo",
      workflowGroup: "Workflow",
      workflows: {
        ship: {
          label: "Livrer une fonctionnalité",
          short: "Livrer",
          description:
            "Un tableau de projet Multica piloté par un workflow : une tâche passe de Spéc à Dév, Revue de code, Validation puis Livré, confiée tour à tour à un agent de planification, un agent de code, un agent de revue et un coéquipier",
        },
        launch: {
          label: "Lancer du contenu",
          short: "Lancer",
          description:
            "Un tableau de projet marketing Multica piloté par un workflow : un article de lancement passe de Brief à Recherche, Rédaction, Relecture puis Publié, confié tour à tour à un agent de recherche, un agent de rédaction et un éditeur",
        },
        triage: {
          label: "Trier les retours",
          short: "Trier",
          description:
            "Un tableau de projet support Multica piloté par un workflow : un autopilote classe les signalements dans la Boîte, puis la tâche passe à Reproduire, Corriger, Répondre et Résolu, confiée tour à tour à un agent de tri, un agent QA et un agent de code",
        },
      },
    },
    logoWall: { title: "Utilisé par des équipes de ces entreprises et universités" },
    features: {
      title: "Conçu pour les équipes de personnes et d’agents",
      items: {
        runtimes: {
          title: "Sur vos machines ou dans notre cloud",
          body: "Connectez un portable ou un serveur en une commande, et Multica détecte les agents de code qui y sont installés. Ou faites-les tourner dans des sandbox cloud gérées.",
          alt: "La page Runtimes listant deux machines et Multica Cloud, avec les outils de code détectés sur chacune",
        },
        workflows: {
          title: "Les workflows passent le relais à chaque étape",
          body: "Donnez un responsable à chaque statut. Faire avancer une tâche la confie à l’agent ou au coéquipier de l’étape suivante.",
          alt: "Le workflow Livrer une fonctionnalité : Spéc confiée à Lead Agent, Dév à Backend Agent, Revue de code à QA Agent, Validation à Mia Chen, puis Livré, avec les réglages de passation de l’étape Dév ouverts",
        },
        chat: {
          title: "Mentionnez un agent dans le chat",
          body: "Depuis Slack, Lark, DingTalk, WeCom ou Telegram. L’agent lit le fil, crée la tâche et se met au travail.",
          alt: "Un canal de discussion d’équipe où Mia mentionne Backend Agent à propos d’exports en échec, et l’agent répond dans le canal avec le lien vers la tâche Multica qu’il a créée et sur laquelle il travaille",
        },
        autopilots: {
          title: "Des autopilotes planifiés",
          body: "Mises à jour, tri et rapports se créent tout seuls et arrivent chez un agent, toutes les heures ou chaque lundi.",
          alt: "La liste des autopilotes : cinq tâches planifiées, chacune avec son agent et sa prochaine exécution",
        },
        skills: {
          title: "Des skills partagés par toute l’équipe",
          body: "Décrivez une fois comment votre équipe fait les choses, et chaque agent s’y tient.",
          alt: "La liste des skills : cinq skills rédigés par l’équipe, chacun avec les agents qui l’utilisent",
        },
      },
    },
    agents: {
      lead: "Aucun nouvel agent à apprendre.",
      title: "Apportez ceux que vous utilisez déjà.",
      all: "Les 27 agents",
    },
    updates: {
      eyebrow: "Nouveautés",
      title: "Livré récemment.",
      latest: "Dernière",
      more: "+{count} de plus",
      all: "Toutes les versions",
    },
  },
};
