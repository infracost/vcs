<h3>💰 Infracost report</h3>

Consider fixing these issues, they don't align with your company's FinOps policies & the Well-Architected Framework. **Add a PR comment with `@infracost help` to see how you can dismiss or snooze issues and unblock your PR.**

<table>
  <tr><td colspan="2" width="1000px">Tagging policies</td></tr>
<tr><td colspan="2" title="Blocking failure">
<details open>
<summary>
<b>❌ Require env tag</b>
</summary><br/>

All resources must have an env tag.
</details>
</td></tr>
<tr><td></td><td>

resource [`aws_instance.web`](https://github.com/my-org/my-repo/blob/abc123/main.tf#L10)
  * Missing mandatory tags: `Service`, `Environment`, `Team`. Consider adding to your default tags to avoid adding tags to individual resources.

in project `my-project`
</td></tr>
<tr><td></td><td>

resource [`aws_s3_bucket.data`](https://github.com/my-org/my-repo/blob/abc123/storage.tf#L5)
  * `env` has invalid value `prod`. Must match regex `/^(production|staging|dev)$/`.
  * `tier` has invalid value `gold`. Must be one of `standard`, `premium`.

in projects `my-project`, `other-project`
</td></tr>
<tr><td></td><td>

resource [`aws_ecs_service.api`](https://github.com/my-org/my-repo/blob/abc123/ecs.tf#L20)
  * Dynamically created `aws_ecs_task` resources will not have tag(s) `Service`, `Environment` because tag propagation is not configured. Tag propagation should be configured by setting `propagate_tags` to a valid value (`SERVICE`, `TASK_DEFINITION`)

in project `my-project`
</td></tr>

</table>

<sub>
  This comment will be updated when code changes.
</sub>

