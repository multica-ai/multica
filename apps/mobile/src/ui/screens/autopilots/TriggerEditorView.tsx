import type { ScheduleConfig } from "@multica/core/autopilots/schedule";
import { FormBody, SheetHeader } from "../../components/Form";
import { Screen } from "../../components/Screen";
import { scheduleRepeat } from "../../model/schedule";
import { useStrings } from "../../strings";
import { ScheduleForm, type NextRunsModel } from "./ScheduleForm";

interface TriggerEditorViewProps {
  editing: boolean;
  config: ScheduleConfig;
  onChange: (config: ScheduleConfig) => void;
  nextRuns: NextRunsModel;
  busy: boolean;
  onCancel: () => void;
  onSave: () => void;
}

export function TriggerEditorView({ editing, config, onChange, nextRuns, busy, onCancel, onSave }: TriggerEditorViewProps) {
  const strings = useStrings();
  return (
    <Screen>
      <SheetHeader
        title={strings.schedule.title}
        leading={{ label: strings.common.cancel, onPress: onCancel }}
        trailing={{
          label: editing ? strings.form.save : strings.form.add,
          onPress: onSave,
          disabled: scheduleRepeat(config) === null || nextRuns.state === "error",
          busy,
        }}
      />
      <FormBody>
        <ScheduleForm config={config} onChange={onChange} nextRuns={nextRuns} />
      </FormBody>
    </Screen>
  );
}
