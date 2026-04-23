"use client";

import { useRouter } from "next/navigation";
import { SkillMatrixPage } from "@multica/views/skills";

export default function SkillMatrixRoute() {
  const router = useRouter();

  return (
    <SkillMatrixPage
      onBack={() => {
        router.push("./"); // Navigate back to skills page
      }}
    />
  );
}
