package user.dockerfile.ocm_ds_0001_review_test

import data.user.dockerfile.ocm_ds_0001 as check
import rego.v1

mock_result(msg, _) := msg

pinned := "alpine:3.22@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

# Commands as Trivy parses them: flags in Flags, not in Value.
denied(stages) := r if {
	r := check.deny with input as {"Stages": [{"Commands": cmds} | some cmds in stages]}
		with result.new as mock_result
}

from(image, name) := {"Cmd": "from", "Flags": [], "Value": [image, "AS", name]}

test_copy_from_image_denied if {
	denied([[from(pinned, "base"), {"Cmd": "copy", "Flags": ["--from=busybox:1.37"], "Value": ["/a", "/b"]}]]) == {
		`base image "busybox:1.37" is not pinned by digest`,
	}
}

test_mount_from_image_denied if {
	denied([[from(pinned, "base"), {"Cmd": "run", "Flags": ["--mount=type=bind,from=busybox:1.37,target=/b"], "Value": ["true"]}]]) == {
		`base image "busybox:1.37" is not pinned by digest`,
	}
}

test_copy_from_earlier_stage_allowed if {
	count(denied([
		[from(pinned, "base")],
		[
			{"Cmd": "from", "Flags": [], "Value": ["scratch"]},
			{"Cmd": "copy", "Flags": ["--from=base"], "Value": ["/a", "/b"]},
			{"Cmd": "copy", "Flags": ["--from=0"], "Value": ["/a", "/b"]},
		],
	])) == 0
}
