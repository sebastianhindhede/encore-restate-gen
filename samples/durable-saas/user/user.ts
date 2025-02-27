import { api } from "encore.dev/api";
import { v4 as uuidv4 } from 'uuid';
import { objects, objectClient, services, serviceClient } from "~restate";
import { ObjectContext, ObjectSharedContext, handlers } from "@restatedev/restate-sdk";
import { User, WriteUserRequest, SignupRequest, SignupResponse } from "~types/user";

export const write = async (ctx: ObjectContext, user: WriteUserRequest): Promise<User> => {
    await ctx.set('data', user);
    const newUser = <User>{ ...user, id: ctx.key };
    return newUser;
};

export const read = handlers.object.shared(
    async (ctx: ObjectSharedContext, key: string): Promise<User | null> => {
        const user = await ctx.get(key).then(user => user ? <User>user : null);
        if (!user) return null;
        user.id = ctx.key;
        return user;
    }
);

export const addUser = api(
    { expose: true, method: "POST", path: "/user" },
    async (req: WriteUserRequest): Promise<User> => {
        const id = uuidv4();
        return objectClient(objects.User, id).write(req);
    }
);

export const updateUser = api(
    { expose: true, method: "PUT", path: "/user/:id" },
    async (req: User): Promise<User> => {
        return objectClient(objects.User, req.id).write(req);
    }
);

export const getUser = api(
    { expose: true, method: "GET", path: "/user/:id" },
    async ({ id }: { id: string }): Promise<User> => {
        if (!id) return <User>{};
        return objectClient(objects.User, id).read('data').then(user => user ?? <User>{});
    }
);

export const signup = api(
    { expose: true, method: "POST", path: "/user/signup" },
    async (req: SignupRequest): Promise<SignupResponse> => {
        const submission = await serviceClient(services.UserManager).signupUser(req);
        return submission;
    }
);

export const confirmEmail = api(
    { expose: true, method: "GET", path: "/user/confirm/:workflowId/:secret" },
    async ({ workflowId, secret }: { workflowId: string, secret: string }): Promise<void> => {
        return serviceClient(services.UserManager).confirmEmail({ workflowId, secret });
    }
);
