# ADR 0021: AWS snapshot scans restore volumes through CSI snapshot APIs

Status: Accepted

An EBS snapshot is not directly mountable. An AWS preparation Job creates and waits for a tagged snapshot; the controller imports its handle through a pre-provisioned `VolumeSnapshotContent` and bound `VolumeSnapshot`, then creates a PVC that restores an EBS volume for a read-only scanner mount. Finalization deletes the PVC and CSI objects, while the cleanup Job deletes the retained cloud snapshot.

AWS snapshot scanning therefore explicitly requires the EBS CSI driver, external snapshot controller, compatible `VolumeSnapshotClass` and `StorageClass` configuration, and scanner capacity in the target region. The operator reports an unsupported-policy condition instead of attempting ad hoc PV or attachment behavior when those prerequisites are absent.
