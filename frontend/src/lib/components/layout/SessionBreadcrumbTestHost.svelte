<script lang="ts">
  import type { Session } from "../../api/types.js";
  // @ts-ignore
  import SessionBreadcrumb from "./SessionBreadcrumb.svelte";

  interface Props {
    initialSession: Session;
  }

  let { initialSession }: Props = $props();
  let session = $state<Session | undefined>(undefined);

  $effect(() => {
    session = initialSession;
  });

  export function setSession(nextSession: Session) {
    session = nextSession;
  }
</script>

<SessionBreadcrumb {session} onBack={() => {}} />
