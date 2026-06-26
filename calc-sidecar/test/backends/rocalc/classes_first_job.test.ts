// Class-load lock-in checks for the first-job tier. See classes.shared.ts for
// the shared class lists, tier-sharding rationale, and check logic.
import { runClassLoadChecks, FIRST_JOB } from "./classes.shared.ts";

runClassLoadChecks(FIRST_JOB);
