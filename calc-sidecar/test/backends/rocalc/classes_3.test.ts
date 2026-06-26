// Class-load lock-in checks, shard 3 of 5. See classes.shared.ts for the
// shared class list, sharding rationale, and check logic.
import { runClassLoadChecks, shard } from "./classes.shared.ts";

runClassLoadChecks(shard(2, 5));
