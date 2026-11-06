"use client";

import { GitBranch, Layers, Search, Users } from "@multica/ui/icons";
import {
  FeatureIntro,
  FeatureIntroCarousel,
  IntroArtCard,
} from "../../layout/feature-intro";
import { useT } from "../../i18n";
import { RegisterRepositoryDialog } from "./register-repository-dialog";

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
      icon: GitBranch,
      title: t(($) => $.code_graph.intro.import_title),
      description: t(($) => $.code_graph.intro.import_help),
    },
    {
      id: "scope",
      icon: Users,
      title: t(($) => $.code_graph.intro.scope_title),
      description: t(($) => $.code_graph.intro.scope_help),
    },
    {
      id: "query",
      icon: Search,
      title: t(($) => $.code_graph.intro.query_title),
      description: t(($) => $.code_graph.intro.query_help),
    },
  ];
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
      action={canManage ? <RegisterRepositoryDialog wsId={wsId} /> : null}
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
          art: (
            <IntroArtCard>
              <div className="flex min-h-40 items-center justify-center">
                <slide.icon className="size-16 text-brand" aria-hidden="true" />
              </div>
            </IntroArtCard>
          ),
        }))}
      />
    </FeatureIntro>
  );
}
