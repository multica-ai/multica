import { Pressable, RefreshControl, ScrollView, StyleSheet, View } from "react-native";
import { Avatar, type AvatarActor } from "../../components/Avatar";
import { Chip, ChipGroup } from "../../components/Chip";
import { Capsule } from "../../components/Floating";
import { PushHeader } from "../../components/Header";
import { IssueRow, type IssueRowModel } from "../../components/IssueRow";
import { Screen } from "../../components/Screen";
import { SectionTitle } from "../../components/SectionTitle";
import { Failure, Loading } from "../../components/States";
import { Txt } from "../../components/Text";
import { Tile } from "../../components/Tile";
import { Icon } from "../../icons/Icon";
import { useStrings } from "../../strings";
import { useTheme } from "../../theme";

export interface SquadModel {
  name: string;
  description: string;
  instructions: string;
  leader: AvatarActor;
  archived: boolean;
}

export interface SquadMemberModel {
  /** `member:<id>` or `agent:<id>`. */
  key: string;
  actor: AvatarActor;
  role: string;
  leader: boolean;
}

interface SquadViewProps {
  state: "loading" | "error" | "ready";
  squad: SquadModel | null;
  members: readonly SquadMemberModel[];
  recent: { state: "loading" | "error" | "ready"; issues: readonly IssueRowModel[] };
  canManage: boolean;
  refreshing?: boolean;
  onRefresh?: () => void;
  onBack: () => void;
  onRetry: () => void;
  onMore: () => void;
  onAddMember: () => void;
  onMember: (member: SquadMemberModel) => void;
  onOpenIssue: (issue: IssueRowModel) => void;
}

export function SquadView(props: SquadViewProps) {
  const { state, squad, members, recent, canManage, refreshing = false, onRefresh, onBack, onRetry, onMore } = props;
  const strings = useStrings();
  const palette = useTheme();
  return (
    <Screen>
      <PushHeader
        onBack={onBack}
        backLabel={strings.common.back}
        right={state === "ready" ? <Capsule actions={[{ icon: "MoreHorizontal", label: strings.common.more, onPress: onMore }]} /> : undefined}
      />
      {state === "loading" ? (
        <Loading />
      ) : state === "error" || !squad ? (
        <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
      ) : (
        <ScrollView
          contentContainerStyle={styles.content}
          refreshControl={onRefresh ? <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor={palette.label3} /> : undefined}
        >
          <Tile icon="Users" size={52} />
          <Txt variant="heading" style={styles.title}>
            {squad.name}
          </Txt>
          <ChipGroup>
            <Chip icon={<Icon name="Crown" size={15} color={palette.warning} />} label={squad.leader.name} />
            <Chip icon={<Icon name="Users" size={15} color={palette.label2} />} label={strings.squad.memberCount(members.length)} />
            {squad.archived ? <Chip icon={<Icon name="Archive" size={15} color={palette.label2} />} label={strings.autopilot.status.archived} /> : null}
          </ChipGroup>
          {squad.description ? (
            <Txt variant="body" style={styles.description}>
              {squad.description}
            </Txt>
          ) : null}

          {squad.instructions ? (
            <View style={styles.block}>
              <SectionTitle>{strings.squad.instructions}</SectionTitle>
              <Txt variant="callout" tone="secondary">
                {squad.instructions}
              </Txt>
            </View>
          ) : null}

          <View style={styles.block}>
            <SectionTitle action={canManage ? { label: strings.form.add, onPress: props.onAddMember } : undefined}>{strings.squad.members}</SectionTitle>
            {members.map((member) => (
              <Pressable
                key={member.key}
                accessibilityRole={canManage ? "button" : undefined}
                disabled={!canManage}
                onPress={() => props.onMember(member)}
                style={({ pressed }) => [styles.member, pressed && { opacity: 0.6 }]}
              >
                <Avatar actor={member.actor} size={32} />
                <View style={styles.flex}>
                  <View style={styles.nameLine}>
                    <Txt variant="row" numberOfLines={1}>
                      {member.actor.name}
                    </Txt>
                    {member.leader ? <Icon name="Crown" size={14} color={palette.warning} /> : null}
                  </View>
                  {member.role ? (
                    <Txt variant="meta" tone="secondary" numberOfLines={1}>
                      {member.leader && member.role === "leader" ? strings.squad.leader : member.role}
                    </Txt>
                  ) : null}
                </View>
                {canManage ? <Icon name="MoreHorizontal" size={18} color={palette.label3} /> : null}
              </Pressable>
            ))}
          </View>

          <View style={styles.block}>
            <SectionTitle>{strings.squad.recent}</SectionTitle>
            <View style={styles.issues}>
              {recent.state === "loading" ? (
                <Loading />
              ) : recent.state === "error" ? (
                <Failure title={strings.common.loadFailed} retryLabel={strings.common.retry} onRetry={onRetry} />
              ) : recent.issues.length === 0 ? (
                <Txt variant="callout" tone="tertiary" style={styles.none}>
                  {strings.squad.noRecent}
                </Txt>
              ) : (
                recent.issues.map((issue) => <IssueRow key={issue.id} issue={issue} onPress={props.onOpenIssue} />)
              )}
            </View>
          </View>
        </ScrollView>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1, minWidth: 0 },
  content: { paddingHorizontal: 20, paddingTop: 4, paddingBottom: 60 },
  title: { marginTop: 12, marginBottom: 14 },
  description: { marginTop: 16 },
  block: { marginTop: 28 },
  member: { flexDirection: "row", alignItems: "center", gap: 12, paddingVertical: 7 },
  nameLine: { flexDirection: "row", alignItems: "center", gap: 6 },
  issues: { marginHorizontal: -20 },
  none: { paddingHorizontal: 20 },
});
