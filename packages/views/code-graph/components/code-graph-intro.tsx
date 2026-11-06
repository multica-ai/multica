"use client";

import { Layers } from "@multica/ui/icons";
import { FeatureIntro, FeatureIntroCarousel } from "../../layout/feature-intro";
import { useT } from "../../i18n";
import { RegisterRepositoryDialog } from "./register-repository-dialog";
import { GitCredentialManager } from "./git-credentials";
import { CodeGraphIntroArt } from "./code-graph-intro-art";

export function CodeGraphIntro({
  wsId,
  canManage,
}: {
  wsId: string;
  canManage: boolean;
}) {
  const { t } = useT("layout");
  const slides = [
    {
      id: "import",
      title: t(($) => $.code_graph.intro.import_title),
      description: t(($) => $.code_graph.intro.import_help),
    },
    {
      id: "scope",
      title: t(($) => $.code_graph.intro.scope_title),
      description: t(($) => $.code_graph.intro.scope_help),
    },
    {
      id: "query",
      title: t(($) => $.code_graph.intro.query_title),
      description: t(($) => $.code_graph.intro.query_help),
    },
  ] as const;
  return (
    <FeatureIntro
      icon={Layers}
      label={t(($) => $.nav.code_graph)}
      title={t(($) => $.code_graph.intro.title)}
      description={t(($) => $.code_graph.intro.description)}
      docs="codegraph"
      columns={[
        t(($) => $.code_graph.repository),
        t(($) => $.code_graph.branch),
        t(($) => $.code_graph.status),
      ]}
      action={
        canManage ? (
          <>
            <RegisterRepositoryDialog wsId={wsId} />
            <GitCredentialManager />
          </>
        ) : null
      }
      footer={
        !canManage ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.code_graph.intro.admin_help)}
          </p>
        ) : undefined
      }
    >
      <FeatureIntroCarousel
        label={t(($) => $.code_graph.intro.title)}
        slides={slides.map((slide) => ({
          ...slide,
          interactiveArt: true,
          art: <CodeGraphIntroArt step={slide.id} title={slide.title} />,
        }))}
      />
    </FeatureIntro>
  );
}
