// Class-load lock-in checks for the second-job tier. See classes.shared.ts for
// the shared class lists, tier-sharding rationale, and check logic.
import { runClassLoadChecks, SECOND_JOB } from "./classes.shared.ts";

runClassLoadChecks(SECOND_JOB);
