// Mirrors the multi-step SOAR playbook engine (Backlog B7, internal/playbook + internal/api).
// A playbook is a named ordered sequence of steps; a run is one execution that auto-runs the
// side-effect-light steps and pauses (awaiting_approval) at each response_action for human approval.

export type PlaybookStepType = 'notify' | 'comment' | 'response_action';

export interface PlaybookStep {
  type: PlaybookStepType;
  // notify
  channel?: string;
  message?: string;
  // comment
  text?: string;
  // response_action
  integration_type?: string;
  action_type?: string;
  target?: string;
  target_from?: string;
}

export interface Playbook {
  id: string;
  name: string;
  description: string;
  steps: PlaybookStep[];
  enabled: boolean;
  created_at: string;
}

export interface PlaybookRunStep {
  step_index: number;
  step_type: string;
  status: string;
  output: string;
}

export interface PlaybookRun {
  id: string;
  playbook_id: string;
  incident_id?: string;
  status: string; // running, awaiting_approval, completed, failed, rejected
  current_step: number;
  started_by: string;
  created_at: string;
  steps?: PlaybookRunStep[];
}
