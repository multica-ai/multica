type UseCasePageLike = {
  slugs: readonly string[];
};

function pageKey(page: UseCasePageLike): string {
  return page.slugs.join("/");
}

export function mergeUseCasePagesWithEnglishFallback<
  TPage extends UseCasePageLike,
>(localizedPages: TPage[], englishPages: TPage[]): TPage[] {
  const localizedSlugs = new Set(localizedPages.map(pageKey));

  return [
    ...localizedPages,
    ...englishPages.filter((page) => !localizedSlugs.has(pageKey(page))),
  ];
}
