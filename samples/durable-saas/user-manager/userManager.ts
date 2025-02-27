import { Context } from "@restatedev/restate-sdk";
import { objects, workflows } from "~restate";
import { WriteUserRequest, SignupRequest, SignupResponse } from "~types/user";

export const signupUser = async (ctx: Context, req: SignupRequest): Promise<SignupResponse> => { 
    const user = await ctx.objectClient<typeof objects.User>(objects.User, ctx.rand.uuidv4()).write(<WriteUserRequest>{
        ...req,
        confirmed: false,
    });
    const workflowId = ctx.rand.uuidv4();
    await ctx.workflowSendClient<typeof workflows.User>(workflows.User, workflowId).run(user);
    return {
        workflow_id: workflowId,
        user,
    };
};

export const confirmEmail = async (ctx: Context, { workflowId, secret }: { workflowId: string, secret: string }): Promise<void> => {
    return ctx.workflowClient<typeof workflows.User>(workflows.User, workflowId).approveEmail(secret);
}